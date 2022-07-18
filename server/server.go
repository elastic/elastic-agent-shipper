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

	"github.com/gofrs/uuid"
)

type Publisher interface {
	io.Closer

	// AcceptedIndex returns the current sequential index of the accepted events
	AcceptedIndex() queue.EntryID
	// AcceptedIndex returns the current sequential index of the persisted events
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

	uuid           string
	persistedIndex uint64

	polling       polling
	notifications notifications

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

	s := shipperServer{
		uuid:      id.String(),
		logger:    logp.NewLogger("shipper-server"),
		publisher: publisher,
		cfg:       cfg,
		polling: polling{
			stopped: make(chan struct{}),
		},
		notifications: notifications{
			subscribers: make(map[uuid.UUID]chan change),
			mutex:       &sync.Mutex{},
		},
		close: &sync.Once{},
	}

	s.polling.ctx, s.polling.stop = context.WithCancel(context.Background())
	s.startPolling()

	return &s, nil
}

// GetAcceptedIndex atomically reads the accepted index
func (serv *shipperServer) GetAcceptedIndex() uint64 {
	return uint64(serv.publisher.AcceptedIndex())
}

// GetPersistedIndex atomically reads the persisted index
func (serv *shipperServer) GetPersistedIndex() uint64 {
	return atomic.LoadUint64(&serv.persistedIndex)
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
	serv.logger.Debug("subscribing client for persisted index change notification...")
	ch, stop, err := serv.notifications.subscribe()
	if err != nil {
		return err
	}

	serv.logger.Debug("client subscribed for persisted index change notifications")
	// defer works in a LIFO order
	defer serv.logger.Debug("client unsubscribed from persisted index change notifications")
	defer stop()

	// before reading notification we send the current values
	// in case the notification would not come in a long time
	err = producer.Send(&messages.PersistedIndexReply{
		Uuid:           serv.uuid,
		PersistedIndex: serv.GetPersistedIndex(),
	})
	if err != nil {
		return err
	}

	for {
		select {

		case change, open := <-ch:
			if !open || change.persistedIndex == nil {
				continue
			}

			err := producer.Send(&messages.PersistedIndexReply{
				Uuid:           serv.uuid,
				PersistedIndex: *change.persistedIndex,
			})
			if err != nil {
				return fmt.Errorf("failed to send the update: %w", err)
			}

		case <-producer.Context().Done():
			return fmt.Errorf("producer context: %w", producer.Context().Err())

		case <-serv.polling.ctx.Done():
			return fmt.Errorf("server is stopped: %w", serv.polling.ctx.Err())
		}
	}
}

// Close implements the Closer interface
func (serv *shipperServer) Close() error {
	serv.close.Do(func() {
		// polling must be stopped first, otherwise it would try to write
		// a notification to a closed channel and this would cause a panic
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
				serv.logger.Debug("updating indices...")
				err := serv.updateIndices(serv.polling.ctx)
				if err != nil {
					serv.logger.Errorf("failed to update indices: %s", err)
				} else {
					serv.logger.Debug("successfully updated indices.")
				}
			}
		}
	}()
}

// updateIndices updates in-memory indices and notifies subscribers if necessary.
func (serv *shipperServer) updateIndices(ctx context.Context) error {
	c := change{}

	oldPersistedIndex := serv.GetPersistedIndex()
	persistedIndex := uint64(serv.publisher.PersistedIndex())

	if persistedIndex != oldPersistedIndex {
		atomic.StoreUint64(&serv.persistedIndex, persistedIndex)
		// register the change
		c.persistedIndex = &persistedIndex
		serv.logger.Debugf("new persisted index %d received", persistedIndex)
	}

	if c.Any() {
		serv.logger.Debug("indices have been updated")

		// this must be async because the loop in `notifyChange` can block on a receiver channel
		go func() {
			serv.logger.Debugf("notifying %d subscribers about change...", len(serv.notifications.subscribers))
			serv.notifications.notify(ctx, c)
			serv.logger.Debug("finished notifying about change")
		}()
	}

	return nil
}
