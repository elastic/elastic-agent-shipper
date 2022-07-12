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

// ShipperServer contains all the gRPC operations for the shipper endpoints.
type ShipperServer interface {
	pb.ProducerServer
	io.Closer
}

type shipperServer struct {
	logger *logp.Logger
	queue  *queue.Queue
	cfg    ShipperServerConfig

	uuid           string
	acceptedIndex  int64
	persistedIndex int64

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
func NewShipperServer(q *queue.Queue, cfg ShipperServerConfig) (ShipperServer, error) {
	if q == nil {
		return nil, errors.New("queue cannot be nil")
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
		uuid:   id.String(),
		logger: logp.NewLogger("shipper-server"),
		queue:  q,
		cfg:    cfg,
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
func (serv *shipperServer) GetAcceptedIndex() int64 {
	return atomic.LoadInt64(&serv.acceptedIndex)
}

// GetPersistedIndex atomically reads the persisted index
func (serv *shipperServer) GetPersistedIndex() int64 {
	return atomic.LoadInt64(&serv.persistedIndex)
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
		serv.logger.
			With(
				"expected", serv.uuid,
				"actual", req.Uuid,
			).
			Debugf("shipper UUID does not match, all events rejected")

		return resp, nil
	}

	for _, e := range req.Events {
		err := serv.queue.Publish(e)
		if err == nil {
			resp.AcceptedIndex++
			continue
		}

		log := serv.logger.
			With(
				"event_count", len(req.Events),
				"accepted_count", resp.AcceptedCount,
			)

		if errors.Is(err, queue.ErrQueueIsFull) {
			log.Warnf("queue is full, not all events accepted")
		} else {
			err = fmt.Errorf("failed to enqueue an event: %w", err)
			serv.logger.Error(err)
		}

		break
	}

	// it is cheaper to check than always using atomic
	if resp.AcceptedCount > 0 {
		atomic.AddInt64(&serv.acceptedIndex, int64(resp.AcceptedCount))
	}
	resp.AcceptedIndex = serv.GetAcceptedIndex()
	resp.PersistedIndex = serv.GetPersistedIndex()

	serv.logger.
		With(
			"event_count", len(req.Events),
			"accepted_count", resp.AcceptedCount,
			"accepted_index", resp.AcceptedIndex,
			"persisted_index", resp.PersistedIndex,
		).
		Debugf("finished publishing a batch")

	return resp, nil
}

// PublishEvents is the server implementation of the gRPC PersistedIndex call.
func (serv *shipperServer) PersistedIndex(req *messages.PersistedIndexRequest, producer pb.Producer_PersistedIndexServer) error {
	// first we reply with current values and then subscribe to notifications and start streaming
	err := producer.Send(&messages.PersistedIndexReply{
		Uuid:           serv.uuid,
		PersistedIndex: serv.GetPersistedIndex(),
	})
	if err != nil {
		return err
	}

	serv.logger.Debug("subscribing client for persisted index change notification...")
	ch, stop, err := serv.notifications.subscribe()
	if err != nil {
		return err
	}

	serv.logger.Debug("client subscribed for persisted index change notifications")
	// defer works in a LIFO order
	defer serv.logger.Debug("client unsubscribed from persisted index change notifications")
	defer stop()

	for {
		select {

		case change := <-ch:
			if change.persistedIndex == nil {
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
			return fmt.Errorf("server context: %w", serv.polling.ctx.Err())
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
// TODO: this is a temporary implementation until the queue supports the `persisted_index`.
func (serv *shipperServer) updateIndices(ctx context.Context) error {

	// TODO: for now we calculate an approximate value
	// we cannot rely on this value and needs to be changed when the queue is ready.
	metrics, err := serv.queue.Metrics()
	if err != nil {
		return fmt.Errorf("failed to fetch queue metrics: %w", err)
	}

	log := serv.logger
	c := change{}

	if metrics.UnackedConsumedEvents.Exists() {
		oldPersistedIndex := serv.GetPersistedIndex()
		persistedIndex := serv.GetAcceptedIndex() - int64(metrics.UnackedConsumedEvents.ValueOr(0))

		if persistedIndex != oldPersistedIndex {
			atomic.SwapInt64(&serv.persistedIndex, persistedIndex)
			// register the change
			c.persistedIndex = &persistedIndex
		}
		log = log.With("persisted_index", persistedIndex)
	}

	log.Debug("indices have been updated")

	if c.Any() {
		log := serv.logger.With(
			"persisted_index", pint64String(c.persistedIndex),
			"subscribers_count", len(serv.notifications.subscribers),
		)

		// this must be async because the loop in `notifyChange` can block on a receiver channel
		go func() {
			log.Debug("notifying about change...")
			serv.notifications.notify(ctx, c)
			log.Debug("finished notifying about change")
		}()
	}

	return nil
}

func pint64String(i *int64) string {
	if i == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *i)
}
