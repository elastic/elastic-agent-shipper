// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/elastic/elastic-agent-libs/logp"

	pb "github.com/elastic/elastic-agent-shipper-client/pkg/proto"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/queue"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/gofrs/uuid"
)

// Publisher contains all operations required for the shipper server to publish incoming events.
type Publisher interface {
	// PersistedIndex returns the current sequential index of the persisted events
	PersistedIndex() (queue.EntryID, error)

	// Publish publishes the given event and returns its index.
	// It blocks until the event is published or the given context is canceled
	// (or the target queue is closed).
	Publish(ctx context.Context, event *messages.Event) (queue.EntryID, error)

	// TryPublish publishes the given event and returns its index.
	// If the event cannot be published without blocking, TryPublish returns an error.
	TryPublish(event *messages.Event) (queue.EntryID, error)
}

// ShipperServer contains all the gRPC operations for the shipper endpoints.
type ShipperServer interface {
	Close() error

	pb.ProducerServer
}

type shipperServer struct {
	logger    *logp.Logger
	publisher Publisher

	uuid string

	close *sync.Once
	ctx   context.Context
	stop  func()

	cfg Config

	pb.UnimplementedProducerServer
}

// NewShipperServer creates a new server instance for handling gRPC endpoints.
func NewShipperServer(cfg Config, publisher Publisher) (ShipperServer, error) {
	if publisher == nil {
		return nil, errors.New("publisher cannot be nil")
	}

	id, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}

	s := shipperServer{
		uuid:      id.String(),
		logger:    logp.NewLogger("shipper-server"),
		publisher: publisher,
		close:     &sync.Once{},
		cfg:       cfg,
	}

	s.ctx, s.stop = context.WithCancel(context.Background())

	return &s, nil
}

// getPersistedIndex returns the persisted index, or 0 if there is an error.
// (An error can only happen if the queue is closed, in which case a placeholder
// of 0 is often sufficient. Callers that need to explicitly recognize an error
// state should use publisher.PersistedIndex directly.)
func (serv *shipperServer) getPersistedIndex() uint64 {
	index, err := serv.publisher.PersistedIndex()
	if err != nil {
		// An error means the queue has been closed, so just return a placeholder.
		return 0
	}
	return uint64(index)
}

// PublishEvents is the server implementation of the gRPC PublishEvents call.
func (serv *shipperServer) PublishEvents(ctx context.Context, req *messages.PublishRequest) (*messages.PublishReply, error) {
	resp := &messages.PublishReply{
		Uuid: serv.uuid,
	}

	// the value in the request is optional
	if req.Uuid != "" && req.Uuid != serv.uuid {
		serv.logger.Debugf("shipper UUID does not match, all events rejected. Expected = %s, actual = %s", serv.uuid, req.Uuid)
		return resp, status.Error(codes.FailedPrecondition, fmt.Sprintf("UUID does not match. Expected = %s, actual = %s", serv.uuid, req.Uuid))
	}

	if len(req.Events) == 0 {
		return resp, nil
	}

	if serv.cfg.StrictMode {
		for _, e := range req.Events {
			err := serv.validateEvent(e)
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
		}
	}

	// we block until at least one event from the batch is published
	acceptedIndex, err := serv.publisher.Publish(ctx, req.Events[0])
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp.AcceptedCount++

	// then we try to publish the rest without blocking
	for _, e := range req.Events[1:] {
		id, err := serv.publisher.TryPublish(e)
		if err == nil {
			resp.AcceptedCount++
			acceptedIndex = id
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

	resp.AcceptedIndex = uint64(acceptedIndex)
	resp.PersistedIndex = serv.getPersistedIndex()

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
	serv.logger.Debug("new subscriber for persisted index change")
	defer serv.logger.Debug("unsubscribed from persisted index change")

	persistedIndex := serv.getPersistedIndex()
	err := producer.Send(&messages.PersistedIndexReply{
		Uuid:           serv.uuid,
		PersistedIndex: persistedIndex,
	})
	if err != nil {
		return err
	}

	pollingIntervalDur := req.PollingInterval.AsDuration()

	if pollingIntervalDur == 0 {
		return nil
	}

	ticker := time.NewTicker(pollingIntervalDur)
	defer ticker.Stop()

	for {
		select {
		case <-producer.Context().Done():
			return fmt.Errorf("producer context: %w", producer.Context().Err())

		case <-serv.ctx.Done():
			return fmt.Errorf("server is stopped: %w", serv.ctx.Err())

		case <-ticker.C:
			newPersistedIndex, err := serv.publisher.PersistedIndex()
			if err != nil || uint64(newPersistedIndex) == persistedIndex {
				continue
			}
			persistedIndex = uint64(newPersistedIndex)
			err = producer.Send(&messages.PersistedIndexReply{
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
		serv.stop()
	})

	return nil
}

func (serv *shipperServer) validateEvent(m *messages.Event) error {
	var msgs []string

	if err := m.Timestamp.CheckValid(); err != nil {
		msgs = append(msgs, fmt.Sprintf("timestamp: %s", err))
	}

	if err := serv.validateDataStream(m.DataStream); err != nil {
		msgs = append(msgs, fmt.Sprintf("datastream: %s", err))
	}

	if err := serv.validateSource(m.Source); err != nil {
		msgs = append(msgs, fmt.Sprintf("source: %s", err))
	}

	if len(msgs) == 0 {
		return nil
	}

	return errors.New(strings.Join(msgs, "; "))
}

func (serv *shipperServer) validateSource(s *messages.Source) error {
	if s == nil {
		return fmt.Errorf("cannot be nil")
	}

	var msgs []string
	if s.InputId == "" {
		msgs = append(msgs, "input_id is a required field")
	}

	if len(msgs) == 0 {
		return nil
	}

	return errors.New(strings.Join(msgs, "; "))
}

func (serv *shipperServer) validateDataStream(ds *messages.DataStream) error {
	if ds == nil {
		return fmt.Errorf("cannot be nil")
	}

	var msgs []string
	if ds.Dataset == "" {
		msgs = append(msgs, "dataset is a required field")
	}
	if ds.Namespace == "" {
		msgs = append(msgs, "namespace is a required field")
	}
	if ds.Type == "" {
		msgs = append(msgs, "type is a required field")
	}

	if len(msgs) == 0 {
		return nil
	}

	return errors.New(strings.Join(msgs, "; "))
}
