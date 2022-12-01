// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package queue

import (
	"context"
	"fmt"
	"sync/atomic"

	beatsqueue "github.com/elastic/beats/v7/libbeat/publisher/queue"
	diskqueue "github.com/elastic/beats/v7/libbeat/publisher/queue/diskqueue"
	memqueue "github.com/elastic/beats/v7/libbeat/publisher/queue/memqueue"
	"github.com/elastic/elastic-agent-libs/logp"

	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

// Queue is a shipper-specific wrapper around the bare libbeat queue.
// It accepts api.Event instead of bare interface pointers like the
// libbeat queue, and it sets opinionated defaults for the queue
// semantics. The intention is to keep the shipper from becoming too
// entangled with the legacy queue api, and to gradually expose more
// features as the libbeat queue evolves and we decide what we want
// to support in the shipper.
type Queue struct {
	config Config

	eventQueue beatsqueue.Queue

	producer beatsqueue.Producer
}

type Metrics beatsqueue.Metrics

// EntryID is a unique ascending id assigned to each entry that goes in the
// queue, to handle acknowledgments within the shipper and report progress
// to the client.
type EntryID beatsqueue.EntryID

// metricsSource is a wrapper around the libbeat queue interface, exposing only
// the callback to query the current metrics. It is used to pass queue metrics
// to the monitoring package.
type MetricsSource interface {
	Metrics() (Metrics, error)
}

var (
	ErrQueueIsFull   = fmt.Errorf("couldn't publish: queue is full")
	ErrQueueIsClosed = fmt.Errorf("couldn't publish: queue is closed")
)

func New(c Config) (*Queue, error) {
	var eventQueue beatsqueue.Queue
	// If both Disk & Mem settings exist, go with Disk
	if c.useDiskQueue() {
		var err error
		diskSettings, err := DiskSettingsFromConfig(c.DiskConfig)
		if err != nil {
			return nil, fmt.Errorf("error creating diskqueue settings: %w", err)
		}
		eventQueue, err = diskqueue.NewQueue(logp.L(), diskSettings)
		if err != nil {
			return nil, fmt.Errorf("error creating diskqueue: %w", err)
		}
	} else {
		eventQueue = memqueue.NewQueue(logp.L(), *c.MemSettings)
	}
	producer := eventQueue.Producer(beatsqueue.ProducerConfig{})
	return &Queue{config: c, eventQueue: eventQueue, producer: producer}, nil
}

func (queue *Queue) Publish(ctx context.Context, event *messages.Event) (EntryID, error) {
	// TODO pass the real channel once libbeat supports it
	id, published := queue.producer.Publish(event /*, ctx.Done()*/)
	if !published {
		return EntryID(0), ErrQueueIsClosed
	}
	return EntryID(id), nil
}

func (queue *Queue) TryPublish(event *messages.Event) (EntryID, error) {
	id, published := queue.producer.TryPublish(event)
	if !published {
		return EntryID(0), ErrQueueIsFull
	}
	return EntryID(id), nil
}

func (queue *Queue) Metrics() (Metrics, error) {
	metrics, err := queue.eventQueue.Metrics()
	// We need to do the explicit cast, otherwise this isn't recognized as the same type
	return Metrics(metrics), err
}

func (queue *Queue) Get(eventCount int) (*WrappedBatch, error) {
	batch, err := queue.eventQueue.Get(eventCount)
	if err != nil {
		return nil, err
	}
	return &WrappedBatch{batch: batch}, nil
}

func (queue *Queue) Close() error {
	return queue.eventQueue.Close()
}

func (queue *Queue) PersistedIndex() (EntryID, error) {
	if queue.config.useDiskQueue() {
		// TODO (https://github.com/elastic/elastic-agent-shipper/issues/27):
		// Once the disk queue supports entry IDs, this should return the
		// ID of the oldest entry that has not yet been written to disk.
		return EntryID(0), nil
	} else {
		metrics, err := queue.eventQueue.Metrics()
		if err != nil {
			return EntryID(0), err
		}
		// When a memory queue event is persisted, it is removed from the queue,
		// so we return the oldest remaining entry ID.
		return EntryID(metrics.OldestEntryID), nil
	}
}

// WrappedBatch is a bookkeeping wrapper around a libbeat queue batch,
// to work around the fact that shipper acknowledgements are per-event
// while the queue can only track an entire batch at a time.
// The plan is to eliminate WrappedBatch once batch assembly / acknowledgment
// is moved out of the libbeat queue.
type WrappedBatch struct {
	batch beatsqueue.Batch

	// how many events from the batch have been acknowledged
	doneCount uint64

	// If CompletionCallback is non-nil, wrappedBatch will call it
	// when all events have been consumed.
	CompletionCallback func()
}

func (w *WrappedBatch) Events() []*messages.Event {
	events := make([]*messages.Event, w.batch.Count())
	for i := 0; i < w.batch.Count(); i++ {
		events[i], _ = w.batch.Entry(i).(*messages.Event)
	}
	return events
}

func (w *WrappedBatch) Done(count uint64) {
	if atomic.AddUint64(&w.doneCount, count) >= uint64(w.batch.Count()) {
		w.batch.Done()
		if w.CompletionCallback != nil {
			w.CompletionCallback()
		}
	}
}
