// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package grpcserver

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

	// IsInitialized reports if the queue can accept events
	IsInitialized() bool
}

// ShipperServer contains all the gRPC operations for the shipper endpoints.
type ShipperServer interface {
	Close() error
	SetStrictMode(bool)
	SetInitError(msg string)

	pb.ProducerServer
}

type shipperServer struct {
	logger    *logp.Logger
	publisher Publisher

	uuid string

	close *sync.Once
	ctx   context.Context
	stop  func()

	strictMode bool
	initErrMsg string

	pb.UnimplementedProducerServer
}

// NewShipperServer creates a new server instance for handling gRPC endpoints.
// publisher can be set to nil, in which case the SetOutput() method must be called.
func NewShipperServer(publisher Publisher) (ShipperServer, error) {
	log := logp.NewLogger("shipper-server")
	if publisher == nil {
		log.Debugf("gRPC endpoint has no output, will wait for config")
	}

	id, err := uuid.NewV4()
	if err != nil {
		return nil, fmt.Errorf("error generating shipper UUID: %w", err)
	}

	s := shipperServer{
		uuid:       id.String(),
		logger:     log,
		publisher:  publisher,
		close:      &sync.Once{},
		strictMode: false,
	}

	s.ctx, s.stop = context.WithCancel(context.Background())

	return &s, nil
}

// SetStrictMode updates the strict mode setting for the server
func (serv *shipperServer) SetStrictMode(mode bool) {
	serv.strictMode = mode
}

// SetInitError sets an error message that will be reported if IsInitialized() is set to false
// helpful for communicating errors in cases where the gRPC server is up, but something else has failed
func (serv *shipperServer) SetInitError(msg string) {
	serv.initErrMsg = msg
}

// PublishEvents is the server implementation of the gRPC PublishEvents call.
func (serv *shipperServer) PublishEvents(ctx context.Context, req *messages.PublishRequest) (*messages.PublishReply, error) {
	if !serv.publisher.IsInitialized() {
		return nil, status.Error(codes.Unavailable, serv.initMessage())
	} else if serv.initErrMsg != "" { // reset error message once we're done initializing
		serv.initErrMsg = ""
	}

	resp := &messages.PublishReply{
		Uuid: serv.uuid,
	}

	// the value in the request is optional
	if req.Uuid != "" && req.Uuid != serv.uuid {
		serv.logger.Debugf("shipper UUID does not match, all events rejected. Expected = %s, actual = %s", serv.uuid, req.Uuid)
		return resp, status.Error(codes.FailedPrecondition, fmt.Sprintf("UUID does not match. Expected = %s, actual = %s", serv.uuid, req.Uuid))
	}

	if len(req.Events) == 0 {
		return nil, status.Error(codes.InvalidArgument, "publish request must contain at least one event")
	}

	if serv.strictMode {
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

	serv.logger.
		Debugf("finished publishing a batch. Events = %d, accepted = %d, accepted index = %d",
			len(req.Events),
			resp.AcceptedCount,
			resp.AcceptedIndex,
		)

	return resp, nil
}

// PublishEvents is the server implementation of the gRPC PersistedIndex call.
func (serv *shipperServer) PersistedIndex(req *messages.PersistedIndexRequest, producer pb.Producer_PersistedIndexServer) error {
	if !serv.publisher.IsInitialized() {
		return status.Error(codes.Unavailable, serv.initMessage())
	} else if serv.initErrMsg != "" { // reset error message once we're done initializing
		serv.initErrMsg = ""
	}
	serv.logger.Debug("new subscriber for persisted index change")
	defer serv.logger.Debug("unsubscribed from persisted index change")

	persistedIndex, err := serv.publisher.PersistedIndex()
	if err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	err = producer.Send(&messages.PersistedIndexReply{
		Uuid:           serv.uuid,
		PersistedIndex: uint64(persistedIndex),
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
			if err != nil || newPersistedIndex == persistedIndex {
				continue
			}
			persistedIndex = newPersistedIndex
			err = producer.Send(&messages.PersistedIndexReply{
				Uuid:           serv.uuid,
				PersistedIndex: uint64(persistedIndex),
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

func (serv *shipperServer) initMessage() string {
	if serv.initErrMsg == "" {
		return "shipper is initializing"
	}
	return serv.initErrMsg
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
