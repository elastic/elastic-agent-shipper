// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package reporter

import (
	"github.com/elastic/elastic-agent-libs/opt"
)

// The reporter interface allows for the creation of new metrics outputs that can sent metrics to any user-configured output. Expvar, logs, etc

// QueueMetrics represents the queue health metrics to be reported to the user, the queue.Metrics struct is "closer to the code", while this is meant to represent useful and human-readable metrics in a stable interface.
// We're keeping this as a struct to prevent the metrics from becoming a free-for-all; the values here should be as self-documenting as possible, and give users quick insight into the state of the queue.
type QueueMetrics struct {
	//QueueLimitReachedCount is the number of times the queue has reached its user-configured limit.
	QueueLimitReachedCount opt.Uint `struct:"queue_limit_reached_count,omitempty"`
	//QueueIsCurrentlyFull reports if the queue is currently at its user-configured limit
	QueueIsCurrentlyFull bool `struct:"queue_is_currently_full,omitempty"`
	//CurrentQueueLevel reports the current fill state of the queue, in the native user-configured limits of the queue
	CurrentQueueLevel opt.Uint `struct:"current_queue_level,omitempty"`
	//QueueMaxLevel reports the user-configured max level of the queue, in the native user-configured limiits
	QueueMaxLevel opt.Uint `struct:"max_queue_level,omitempty"`
	//OldestActiveEvent reports the timestamp of the oldest event in the queue
	OldestActiveEvent string `struct:"oldest_active_event,omitempty"`
}

// Reporter is the bare interface that will be implemented by the various outputs
type Reporter interface {
	Update(QueueMetrics) error
}
