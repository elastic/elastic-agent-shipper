// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package queue

import (
	"fmt"

	beatsqueue "github.com/elastic/beats/v7/libbeat/publisher/queue"

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

	//producer beatsqueue.Producer
}

type Metrics beatsqueue.Metrics

// metricsSource is a wrapper around the libbeat queue interface, exposing only
// the callback to query the current metrics. It is used to pass queue metrics
// to the monitoring package.
type MetricsSource interface {
	Metrics() (Metrics, error)
}

func New() (*Queue, error) {
	return &Queue{}, nil
}

func (queue *Queue) Publish(event *api.Event) error {
	return fmt.Errorf("couldn't publish: Queue.Publish is not implemented")
}

func (queue *Queue) Metrics() (Metrics, error) {

	//metrics, err := queue.eventQueue.Metrics() // this won't run without the actual queue yet
	// We need to do the explicit cast, otherwise this isn't recognized as the same type
	return Metrics(beatsqueue.Metrics{}), nil
}

func (queue *Queue) Close() {

}
