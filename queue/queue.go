// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package queue

import (
	"fmt"

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
	eventQueue beatsqueue.Queue

	producer beatsqueue.Producer
}

type Metrics beatsqueue.Metrics

// EntryID is a unique ascending id assigned to each entry that goes in the
// queue, to handle acknowledgments within the shipper and report progress
// to the client.
type EntryID uint64

// metricsSource is a wrapper around the libbeat queue interface, exposing only
// the callback to query the current metrics. It is used to pass queue metrics
// to the monitoring package.
type MetricsSource interface {
	Metrics() (Metrics, error)
}

var ErrQueueIsFull = fmt.Errorf("couldn't publish: queue is full")

func New(c Config) (*Queue, error) {
	var eventQueue beatsqueue.Queue
	// If both Disk & Mem settings exist, go with Disk
	if c.DiskSettings != nil {
		var err error
		eventQueue, err = diskqueue.NewQueue(logp.L(), *c.DiskSettings)
		if err != nil {
			return nil, fmt.Errorf("error creating diskqueue: %w", err)
		}
	} else {
		eventQueue = memqueue.NewQueue(logp.L(), *c.MemSettings)
	}
	producer := eventQueue.Producer(beatsqueue.ProducerConfig{})
	return &Queue{eventQueue: eventQueue, producer: producer}, nil
}

func (queue *Queue) Publish(event *messages.Event) (EntryID, error) {
	if !queue.producer.Publish(event) {
		return EntryID(0), ErrQueueIsFull
	}
	return EntryID(0), nil
}

func (queue *Queue) Metrics() (Metrics, error) {
	metrics, err := queue.eventQueue.Metrics()
	// We need to do the explicit cast, otherwise this isn't recognized as the same type
	return Metrics(metrics), err
}

func (queue *Queue) Get(eventCount int) (beatsqueue.Batch, error) {
	return queue.eventQueue.Get(eventCount)
}

func (queue *Queue) Close() {
	queue.eventQueue.Close()
}

func (queue *Queue) AcceptedIndex() EntryID {
	return EntryID(0)
}

func (queue *Queue) PersistedIndex() EntryID {
	// This function needs to be implemented differently depending on the queue
	// type. For the memory queue, it should return the most recent sequential
	// entry id that has been published and acknowledged by the outputs.
	// For the disk queue, it should return the most recent sequential entry id
	// that has been written to disk.
	return EntryID(0)
}
