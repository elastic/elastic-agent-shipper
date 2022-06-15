// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package queue

import (
	"fmt"
	"time"

	beatsqueue "github.com/elastic/beats/v7/libbeat/publisher/queue"
	memqueue "github.com/elastic/beats/v7/libbeat/publisher/queue/memqueue"
	"github.com/elastic/elastic-agent-libs/logp"

	"github.com/elastic/elastic-agent-shipper/api"
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

// metricsSource is a wrapper around the libbeat queue interface, exposing only
// the callback to query the current metrics. It is used to pass queue metrics
// to the monitoring package.
type MetricsSource interface {
	Metrics() (Metrics, error)
}

var ErrQueueIsFull = fmt.Errorf("couldn't publish: queue is full")

func New() (*Queue, error) {
	eventQueue := memqueue.NewQueue(logp.L(), memqueue.Settings{
		Events: 1024,
		// The event count and timeout for queue flushes is hard-coded to a placeholder
		// for now, since that's what the existing Beats API understands. The plan is
		// for these parameters to instead be controlled by the output as it reads
		// the queue. See https://github.com/elastic/elastic-agent-shipper/issues/58.
		FlushMinEvents: 256,
		FlushTimeout:   5 * time.Millisecond,
	})
	producer := eventQueue.Producer(beatsqueue.ProducerConfig{})
	return &Queue{eventQueue: eventQueue, producer: producer}, nil
}

func (queue *Queue) Publish(event *api.Event) error {
	if !queue.producer.Publish(event) {
		return ErrQueueIsFull
	}
	return nil
}

func (queue *Queue) Metrics() (Metrics, error) {

	//metrics, err := queue.eventQueue.Metrics() // this won't run without the actual queue yet
	// We need to do the explicit cast, otherwise this isn't recognized as the same type
	return Metrics(beatsqueue.Metrics{}), nil
}

func (queue *Queue) Get(eventCount int) (beatsqueue.Batch, error) {
	return queue.eventQueue.Get(eventCount)
}

func (queue *Queue) Close() {
	queue.eventQueue.Close()
}
