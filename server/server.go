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

	uuid string

	close *sync.Once
	ctx   context.Context
	stop  func()

	pb.UnimplementedProducerServer
}

// NewShipperServer creates a new server instance for handling gRPC endpoints.
func NewShipperServer(publisher Publisher) (ShipperServer, error) {
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
	}

	s.ctx, s.stop = context.WithCancel(context.Background())

	return &s, nil
}

// GetAcceptedIndex returns the accepted index
func (serv *shipperServer) GetAcceptedIndex() uint64 {
	return uint64(serv.publisher.AcceptedIndex())
}

// GetPersistedIndex returns the persisted index
func (serv *shipperServer) GetPersistedIndex() uint64 {
	return uint64(serv.publisher.PersistedIndex())
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

		return resp, status.Error(codes.FailedPrecondition, fmt.Sprintf("UUID does not match. Expected = %s, actual = %s", serv.uuid, req.Uuid))
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
	serv.logger.Debug("new subscriber for persisted index change")
	defer serv.logger.Debug("unsubscribed from persisted index change")

	persistedIndex := serv.GetPersistedIndex()
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
			newPersistedIndex := serv.GetPersistedIndex()
			if newPersistedIndex == persistedIndex {
				continue
			}
			persistedIndex = newPersistedIndex
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
		serv.stop()
	})

	return nil
}
