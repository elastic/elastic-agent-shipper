// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"time"

	"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/beats/v7/libbeat/opt"
	"github.com/elastic/beats/v7/libbeat/publisher/queue"
)

// ===================================== Test code, remove this later

// TestMetricsQueue is a test queue for the reporter
type TestMetricsQueue struct {
	metricState queue.Metrics
}

// NewTestQueue returns a new test queue
func NewTestQueue() *TestMetricsQueue {
	return &TestMetricsQueue{
		metricState: queue.Metrics{
			OldestActiveTimestamp: common.Time(time.Now()),
			EventCount:            opt.UintWith(0),
			EventLimit:            opt.UintWith(50),
		},
	}
}

// BufferConfig doesn't do anything
func (tq TestMetricsQueue) BufferConfig() queue.BufferConfig {
	return queue.BufferConfig{}
}

// Producer doesn't do anything
func (tq TestMetricsQueue) Producer(_ queue.ProducerConfig) queue.Producer {
	return nil
}

// Consumer doesn't do anything
func (tq TestMetricsQueue) Consumer() queue.Consumer {
	return nil
}

// Close Doesn't do anything
func (tq TestMetricsQueue) Close() error {
	return nil
}

// Metrics spoofs the metrics output
func (tq *TestMetricsQueue) Metrics() (queue.Metrics, error) {
	tq.metricState.EventCount = opt.UintWith(tq.metricState.EventCount.ValueOr(0) + 5)
	return tq.metricState, nil
}
