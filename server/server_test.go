// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/elastic/elastic-agent-shipper-client/pkg/helpers"
	pb "github.com/elastic/elastic-agent-shipper-client/pkg/proto"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/queue"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const bufSize = 1024 * 1024 // 1MB

func TestPublish(t *testing.T) {
	ctx := context.Background()

	sampleValues, err := helpers.NewStruct(map[string]interface{}{
		"string": "value",
		"number": 42,
	})

	require.NoError(t, err)

	e := &messages.Event{
		Timestamp: timestamppb.Now(),
		Source: &messages.Source{
			InputId:  "input",
			StreamId: "stream",
		},
		DataStream: &messages.DataStream{
			Type:      "log",
			Dataset:   "default",
			Namespace: "default",
		},
		Metadata: sampleValues,
		Fields:   sampleValues,
	}

	publisher := &publisherMock{
		persistedIndex: 42,
	}
	shipper, err := NewShipperServer(DefaultConfig(), publisher)
	defer func() { _ = shipper.Close() }()
	require.NoError(t, err)
	client, stop := startServer(t, ctx, shipper)
	defer stop()

	// get the current UUID
	pir := getPersistedIndex(t, ctx, client)

	t.Run("should successfully publish a batch", func(t *testing.T) {
		publisher.q = make([]*messages.Event, 0, 3)
		events := []*messages.Event{e, e, e}
		reply, err := client.PublishEvents(ctx, &messages.PublishRequest{
			Uuid:   pir.Uuid,
			Events: events,
		})
		require.NoError(t, err)
		assertIndices(t, reply, pir, len(events), len(events), int(publisher.persistedIndex))
	})

	t.Run("should grow accepted index", func(t *testing.T) {
		publisher.q = make([]*messages.Event, 0, 3)
		events := []*messages.Event{e}
		reply, err := client.PublishEvents(ctx, &messages.PublishRequest{
			Uuid:   pir.Uuid,
			Events: events,
		})
		require.NoError(t, err)
		assertIndices(t, reply, pir, len(events), 1, int(publisher.persistedIndex))
		reply, err = client.PublishEvents(ctx, &messages.PublishRequest{
			Uuid:   pir.Uuid,
			Events: events,
		})
		require.NoError(t, err)
		assertIndices(t, reply, pir, len(events), 2, int(publisher.persistedIndex))
		reply, err = client.PublishEvents(ctx, &messages.PublishRequest{
			Uuid:   pir.Uuid,
			Events: events,
		})
		require.NoError(t, err)
		assertIndices(t, reply, pir, len(events), 3, int(publisher.persistedIndex))
	})

	t.Run("should return different count when queue is full", func(t *testing.T) {
		publisher.q = make([]*messages.Event, 0, 1)
		events := []*messages.Event{e, e, e} // 3 should not fit into the queue size 1
		reply, err := client.PublishEvents(ctx, &messages.PublishRequest{
			Uuid:   pir.Uuid,
			Events: events,
		})
		require.NoError(t, err)
		assertIndices(t, reply, pir, 1, 1, int(publisher.persistedIndex))
	})

	t.Run("should return an error when uuid does not match", func(t *testing.T) {
		publisher.q = make([]*messages.Event, 0, 3)
		events := []*messages.Event{e, e, e}
		reply, err := client.PublishEvents(ctx, &messages.PublishRequest{
			Uuid:   "wrong",
			Events: events,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "UUID does not match")
		require.Nil(t, reply)
	})

	t.Run("should return validation errors", func(t *testing.T) {
		cases := []struct {
			name        string
			event       *messages.Event
			expectedMsg string
		}{
			{
				name: "no timestamp",
				event: &messages.Event{
					Source: &messages.Source{
						InputId:  "input",
						StreamId: "stream",
					},
					DataStream: &messages.DataStream{
						Type:      "log",
						Dataset:   "default",
						Namespace: "default",
					},
					Metadata: sampleValues,
					Fields:   sampleValues,
				},
				expectedMsg: "invalid nil Timestamp",
			},
			{
				name: "no source",
				event: &messages.Event{
					Timestamp: timestamppb.Now(),
					DataStream: &messages.DataStream{
						Type:      "log",
						Dataset:   "default",
						Namespace: "default",
					},
					Metadata: sampleValues,
					Fields:   sampleValues,
				},
				expectedMsg: "source: cannot be nil",
			},
			{
				name: "no input ID",
				event: &messages.Event{
					Timestamp: timestamppb.Now(),
					Source: &messages.Source{
						StreamId: "stream",
					},
					DataStream: &messages.DataStream{
						Type:      "log",
						Dataset:   "default",
						Namespace: "default",
					},
					Metadata: sampleValues,
					Fields:   sampleValues,
				},
				expectedMsg: "source: input_id is a required field",
			},
			{
				name: "no datastream",
				event: &messages.Event{
					Timestamp: timestamppb.Now(),
					Source: &messages.Source{
						InputId:  "input",
						StreamId: "stream",
					},
					Metadata: sampleValues,
					Fields:   sampleValues,
				},
				expectedMsg: "datastream: cannot be nil",
			},
			{
				name: "invalid data stream",
				event: &messages.Event{
					Timestamp: timestamppb.Now(),
					Source: &messages.Source{
						InputId:  "input",
						StreamId: "stream",
					},
					DataStream: &messages.DataStream{},
					Metadata:   sampleValues,
					Fields:     sampleValues,
				},
				expectedMsg: "datastream: dataset is a required field; namespace is a required field; type is a required field",
			},
		}

		publisher.q = make([]*messages.Event, 0, len(cases))

		cfg := Config{
			StrictMode: true, // so we can test the validation
		}

		strictShipper, err := NewShipperServer(cfg, publisher)
		defer func() { _ = strictShipper.Close() }()
		require.NoError(t, err)
		strictClient, stop := startServer(t, ctx, strictShipper)
		defer stop()
		strictPir := getPersistedIndex(t, ctx, strictClient)

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				reply, err := strictClient.PublishEvents(ctx, &messages.PublishRequest{
					Uuid:   strictPir.Uuid,
					Events: []*messages.Event{tc.event},
				})
				require.Error(t, err)
				require.Nil(t, reply)

				status, ok := status.FromError(err)
				require.True(t, ok, "expected gRPC error")
				require.Equal(t, codes.InvalidArgument, status.Code())
				require.Contains(t, status.Message(), tc.expectedMsg)

				// no validation in non-strict mode
				reply, err = client.PublishEvents(ctx, &messages.PublishRequest{
					Uuid:   pir.Uuid,
					Events: []*messages.Event{tc.event},
				})
				require.NoError(t, err)
				require.Equal(t, uint32(1), reply.AcceptedCount, "should accept in non-strict mode")
			})
		}
	})
}

func TestPersistedIndex(t *testing.T) {
	ctx := context.Background()

	publisher := &publisherMock{persistedIndex: 42}

	t.Run("server should send updates to the clients", func(t *testing.T) {
		shipper, err := NewShipperServer(DefaultConfig(), publisher)
		defer func() { _ = shipper.Close() }()
		require.NoError(t, err)
		client, stop := startServer(t, ctx, shipper)
		defer stop()

		// first delivery can happen before the first index update
		require.Eventually(t, func() bool {
			cl := createConsumers(t, ctx, client, 5, 5*time.Millisecond)
			defer cl.stop()
			return cl.assertConsumed(t, 42) // initial value in the publisher
		}, 100*time.Millisecond, time.Millisecond, "clients are supposed to get the update")

		cl := createConsumers(t, ctx, client, 50, 5*time.Millisecond)
		publisher.persistedIndex = 64

		cl.assertConsumed(t, 64)

		publisher.persistedIndex = 128

		cl.assertConsumed(t, 128)

		cl.stop()
	})

	t.Run("server should properly shutdown", func(t *testing.T) {
		shipper, err := NewShipperServer(DefaultConfig(), publisher)
		require.NoError(t, err)
		client, stop := startServer(t, ctx, shipper)
		defer stop()

		cl := createConsumers(t, ctx, client, 50, 5*time.Millisecond)
		publisher.persistedIndex = 64
		shipper.Close() // stopping the server
		require.Eventually(t, func() bool {
			return cl.assertClosedServer(t) // initial value in the publisher
		}, 100*time.Millisecond, time.Millisecond, "server was supposed to shutdown")
	})
}

func startServer(t *testing.T, ctx context.Context, shipperServer ShipperServer) (client pb.ProducerClient, stop func()) {
	lis := bufconn.Listen(bufSize)
	grpcServer := grpc.NewServer()

	pb.RegisterProducerServer(grpcServer, shipperServer)
	go func() {
		_ = grpcServer.Serve(lis)
	}()

	bufDialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}

	conn, err := grpc.DialContext(
		ctx,
		"bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		require.NoError(t, err)
	}

	stop = func() {
		shipperServer.Close()
		conn.Close()
		grpcServer.Stop()
	}

	return pb.NewProducerClient(conn), stop
}

func createConsumers(t *testing.T, ctx context.Context, client pb.ProducerClient, count int, pollingInterval time.Duration) consumerList {
	ctx, cancel := context.WithCancel(ctx)

	cl := consumerList{
		stop:      cancel,
		consumers: make([]pb.Producer_PersistedIndexClient, 0, count),
	}
	for i := 0; i < 50; i++ {
		consumer, err := client.PersistedIndex(ctx, &messages.PersistedIndexRequest{
			PollingInterval: durationpb.New(pollingInterval),
		})
		require.NoError(t, err)
		cl.consumers = append(cl.consumers, consumer)
	}

	return cl
}

func assertIndices(t *testing.T, reply *messages.PublishReply, pir *messages.PersistedIndexReply, acceptedCount int, acceptedIndex int, persistedIndex int) {
	require.NotNil(t, reply, "reply cannot be nil")
	require.Equal(t, uint32(acceptedCount), reply.AcceptedCount, "accepted count does not match")
	require.Equal(t, uint64(acceptedIndex), reply.AcceptedIndex, "accepted index does not match")
	require.Equal(t, uint64(persistedIndex), reply.PersistedIndex, "persisted index does not match")
	require.Equal(t, uint64(persistedIndex), pir.PersistedIndex, "persisted index reply does not match")
}

func getPersistedIndex(t *testing.T, ctx context.Context, client pb.ProducerClient) *messages.PersistedIndexReply {
	pirCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	consumer, err := client.PersistedIndex(pirCtx, &messages.PersistedIndexRequest{})
	require.NoError(t, err)
	pir, err := consumer.Recv()
	require.NoError(t, err)
	return pir
}

type consumerList struct {
	consumers []pb.Producer_PersistedIndexClient
	stop      func()
}

func (l consumerList) assertConsumed(t *testing.T, value uint64) bool {
	for _, c := range l.consumers {
		pir, err := c.Recv()
		require.NoError(t, err)
		if pir.PersistedIndex != value {
			return false
		}
	}
	return true
}

func (l consumerList) assertClosedServer(t *testing.T) bool {
	for _, c := range l.consumers {
		_, err := c.Recv()
		if err == nil {
			return false
		}

		if !strings.Contains(err.Error(), "server is stopped: context canceled") {
			return false
		}
	}

	return true
}

type publisherMock struct {
	Publisher
	q              []*messages.Event
	persistedIndex queue.EntryID
}

func (p *publisherMock) Publish(_ context.Context, event *messages.Event) (queue.EntryID, error) {
	return p.TryPublish(event)
}

func (p *publisherMock) TryPublish(event *messages.Event) (queue.EntryID, error) {
	if len(p.q) == cap(p.q) {
		return queue.EntryID(0), queue.ErrQueueIsFull
	}

	p.q = append(p.q, event)
	return queue.EntryID(len(p.q)), nil
}

func (p *publisherMock) AcceptedIndex() queue.EntryID {
	return queue.EntryID(len(p.q))
}

func (p *publisherMock) PersistedIndex() queue.EntryID {
	return p.persistedIndex
}
