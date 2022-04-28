// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//Package reporter allows for the creation of new metrics outputs that can send metrics to any user-configured output. Expvar, logs, etc
package reporter

import (
	"github.com/elastic/elastic-agent-libs/opt"
)

// QueueMetrics represents the queue health metrics to be reported to the user, the queue.Metrics struct is "closer to the code", while this is meant to represent useful and human-readable metrics in a stable interface.
// We're keeping this as a struct to prevent the metrics from becoming a free-for-all; the values here should be as self-documenting as possible, and give users quick insight into the state of the queue.
type QueueMetrics struct {
	// LimitReachedCount is the number of times the queue has reached its user-configured limit.
	LimitReachedCount opt.Uint `struct:"limit_reached_count,omitempty" json:"limit_reached_count"`
	// IsFull reports if the queue is currently at its user-configured limit
	IsFull bool `struct:"is_full,omitempty" json:"is_full"`
	// CurrentLevel reports the current fill state of the queue, in the native user-configured limits of the queue
	CurrentLevel opt.Uint `struct:"current_level,omitempty" json:"current_level"`
	//MaxLevel reports the user-configured max level of the queue, in the native user-configured limiits
	MaxLevel opt.Uint `struct:"max_level,omitempty" json:"max_level"`
	//OldestActiveTimestamp reports the timestamp of the oldest event in the queue
	OldestActiveTimestamp string `struct:"oldest_active_event,omitempty" json:"oldest_active_event"`
}

// Reporter is the bare interface that will be implemented by the various outputs
type Reporter interface {
	ReportQueueMetrics(QueueMetrics) error
	Close() error
}
