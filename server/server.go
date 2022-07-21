// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elastic/elastic-agent-libs/logp"
	pb "github.com/elastic/elastic-agent-shipper-client/pkg/proto"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/queue"

	"google.golang.org/grpc/peer"

	"github.com/gofrs/uuid"
)

// Publisher contains all operations required for the shipper server to publish incoming events.
type Publisher interface {
	io.Closer

	// AcceptedIndex returns the current sequential index of the accepted events
	AcceptedIndex() queue.EntryID
	// PersistedIndex returns the current sequential index of the persisted events
	PersistedIndex() queue.EntryID
	// Publish publishes the given event and returns the current accepted index (after this event)
	Publish(*messages.Event) (queue.EntryID, error)
}

// ShipperServer contains all the gRPC operations for the shipper endpoints.
type ShipperServer interface {
	pb.ProducerServer
	io.Closer
}

type shipperServer struct {
	logger    *logp.Logger
	publisher Publisher
	cfg       ShipperServerConfig

	uuid string

	polling          polling
	notifications    notifications
	indexSubscribers int64

	close *sync.Once

	pb.UnimplementedProducerServer
}

type polling struct {
	ctx     context.Context
	stop    func()
	stopped chan struct{}
}

// NewShipperServer creates a new server instance for handling gRPC endpoints.
func NewShipperServer(publisher Publisher, cfg ShipperServerConfig) (ShipperServer, error) {
	if publisher == nil {
		return nil, errors.New("publisher cannot be nil")
	}

	err := cfg.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	id, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}

	notificationMutex := &sync.RWMutex{}

	s := shipperServer{
		uuid:      id.String(),
		logger:    logp.NewLogger("shipper-server"),
		publisher: publisher,
		cfg:       cfg,
		polling: polling{
			stopped: make(chan struct{}),
		},
		notifications: notifications{
			mutex: notificationMutex,
			cond:  sync.NewCond(notificationMutex),
		},
		close: &sync.Once{},
	}

	s.polling.ctx, s.polling.stop = context.WithCancel(context.Background())
	s.startPolling()

	return &s, nil
}

// GetAcceptedIndex returns the accepted index
func (serv *shipperServer) GetAcceptedIndex() uint64 {
	return uint64(serv.publisher.AcceptedIndex())
}

// GetPersistedIndex returns the persisted index
func (serv *shipperServer) GetPersistedIndex() uint64 {
	s := serv.notifications.getState()
	if s == nil {
		return 0
	}
	return s.persistedIndex
}

// PublishEvents is the server implementation of the gRPC PublishEvents call.
func (serv *shipperServer) PublishEvents(_ context.Context, req *messages.PublishRequest) (*messages.PublishReply, error) {
	resp := &messages.PublishReply{
		Uuid: serv.uuid,
	}

	// the value in the request is optional
	if req.Uuid != "" && req.Uuid != serv.uuid {
		resp.AcceptedIndex = serv.GetAcceptedIndex()
		resp.PersistedIndex = serv.GetPersistedIndex()
		serv.logger.Debugf("shipper UUID does not match, all events rejected. Expected = %s, actual = %s", serv.uuid, req.Uuid)

		return resp, nil
	}

	for _, e := range req.Events {
		_, err := serv.publisher.Publish(e)
		if err == nil {
			resp.AcceptedCount++
			continue
		}

		if errors.Is(err, queue.ErrQueueIsFull) {
			serv.logger.Debugf("queue is full, not all events accepted. Events = %d, accepted = %d", len(req.Events), resp.AcceptedCount)
		} else {
			err = fmt.Errorf("failed to enqueue an event. Events = %d, accepted = %d: %w", len(req.Events), resp.AcceptedCount, err)
			serv.logger.Error(err)
		}

		break
	}

	resp.AcceptedIndex = serv.GetAcceptedIndex()
	resp.PersistedIndex = serv.GetPersistedIndex()

	serv.logger.
		Debugf("finished publishing a batch. Events = %d, accepted = %d, accepted index = %d, persisted index = %d",
			len(req.Events),
			resp.AcceptedCount,
			resp.AcceptedIndex,
			resp.PersistedIndex,
		)

	return resp, nil
}

// PublishEvents is the server implementation of the gRPC PersistedIndex call.
func (serv *shipperServer) PersistedIndex(req *messages.PersistedIndexRequest, producer pb.Producer_PersistedIndexServer) error {
	atomic.AddInt64(&serv.indexSubscribers, 1)
	defer atomic.AddInt64(&serv.indexSubscribers, -1)

	p, _ := peer.FromContext(producer.Context())
	addr := addressString(p)
	serv.logger.Debugf("%s subscribed for persisted index change", addr)
	defer serv.logger.Debugf("%s unsubscribed from persisted index change", addr)

	// before reading notification we send the current value
	// in case the first notification does not come in a long time
	persistedIndex := serv.GetPersistedIndex()
	if persistedIndex != 0 {
		err := producer.Send(&messages.PersistedIndexReply{
			Uuid:           serv.uuid,
			PersistedIndex: persistedIndex,
		})
		if err != nil {
			return err
		}
	}

	for {
		select {
		case <-producer.Context().Done():
			return fmt.Errorf("producer context: %w", producer.Context().Err())

		case <-serv.polling.ctx.Done():
			return fmt.Errorf("server is stopped: %w", serv.polling.ctx.Err())
		default:
			state := serv.notifications.wait()
			// state can be nil if the notifications are shutting down
			// we don't send notifications about the same index twice
			if state == nil || state.persistedIndex == persistedIndex {
				continue
			}
			persistedIndex = state.persistedIndex // store for future comparison
			err := producer.Send(&messages.PersistedIndexReply{
				Uuid:           serv.uuid,
				PersistedIndex: persistedIndex,
			})
			if err != nil {
				return fmt.Errorf("failed to send the update: %w", err)
			}
		}
	}
}

// Close implements the Closer interface
func (serv *shipperServer) Close() error {
	serv.close.Do(func() {
		// we should stop polling first to cut off the source of notifications
		serv.polling.stop()
		<-serv.polling.stopped

		serv.logger.Debug("shutting down all notifications...")
		serv.notifications.shutdown()
		serv.logger.Debug("all notifications have been shut down")
	})

	return nil
}

func (serv *shipperServer) startPolling() {
	go func() {
		ticker := time.NewTicker(serv.cfg.PollingInterval)
		defer ticker.Stop()

		for {
			select {

			case <-serv.polling.ctx.Done():
				err := serv.polling.ctx.Err()
				if err != nil && errors.Is(err, context.Canceled) {
					serv.logger.Error(err)
				}
				close(serv.polling.stopped) // signaling back to `Close`
				return

			case <-ticker.C:
				serv.trySendNewIndex()
			}
		}
	}()
}

// trySendNewIndex gets the latest indices and notifies subscribers about the new value.
func (serv *shipperServer) trySendNewIndex() {
	persistedIndex := uint64(serv.publisher.PersistedIndex())

	s := state{
		persistedIndex: persistedIndex,
	}
	notified := serv.notifications.notify(s)
	if !notified {
		return
	}
	serv.logger.Debugf("notified %d subscribers about new persisted index %d.", atomic.LoadInt64(&serv.indexSubscribers), persistedIndex)
}

func addressString(p *peer.Peer) string {
	if p == nil {
		return "client"
	}
	return p.Addr.String()
}
