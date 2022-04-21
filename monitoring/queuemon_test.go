// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package monitoring

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/beats/v7/libbeat/opt"
	"github.com/elastic/beats/v7/libbeat/publisher/queue"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-shipper/monitoring/reporter"
)

// ======= mocked test input queue

// TestMetricsQueue is a test queue for the reporter
type TestMetricsQueue struct {
	metricState queue.Metrics
	limit       uint64
}

// NewTestQueue returns a new test queue
func NewTestQueue(limit uint64) *TestMetricsQueue {
	return &TestMetricsQueue{
		metricState: queue.Metrics{
			OldestActiveTimestamp: common.Time(time.Now()),
			EventCount:            opt.UintWith(0),
			EventLimit:            opt.UintWith(limit),
		},
		limit: limit,
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
	tq.metricState.EventCount = opt.UintWith(tq.metricState.EventCount.ValueOr(0) + 1)

	if tq.metricState.EventCount.ValueOr(0) > tq.limit {
		tq.metricState.EventCount = opt.UintWith(0)
	}

	return tq.metricState, nil
}

// ============ mocked output reporter

type TestMetricsReporter struct {
	mon chan reporter.QueueMetrics
}

func NewTestMetricsReporter(_ config.Namespace) (reporter.Reporter, error) {
	// This default init function just has a dummy channel
	return TestMetricsReporter{mon: make(chan reporter.QueueMetrics, 10)}, nil
}

func (tr TestMetricsReporter) Update(data reporter.QueueMetrics) error {
	tr.mon <- data
	return nil
}

func newReporterFactory(outputChan chan reporter.QueueMetrics) reporter.OutputInit {
	return func(_ config.Namespace) (reporter.Reporter, error) {
		return TestMetricsReporter{mon: outputChan}, nil
	}
}

func initMonWithconfig(interval int, t *testing.T) Config {
	testConfig := Config{Interval: time.Second * time.Duration(interval)}
	//Do a little bit of hackiness so we can construct that namespace object
	input := []map[string]interface{}{
		{"test": map[string]bool{"enabled": true}},
	}
	raw, err := config.NewConfigFrom(input)
	assert.NoError(t, err)
	err = raw.Unpack(&testConfig.OutputConfig)
	assert.NoError(t, err)

	return testConfig
}

// actual tests

func TestSetupMonitor(t *testing.T) {
	reporter.RegisterOutput("test", NewTestMetricsReporter)
	monitor := initMonWithconfig(1, t)

	queue := NewTestQueue(10)
	mon, err := NewFromConfig(monitor, queue)
	assert.NoError(t, err)
	mon.Watch()
	time.Sleep(time.Second)
	mon.End()

}

func TestReportedEvents(t *testing.T) {
	outChan := make(chan reporter.QueueMetrics)
	reporter.RegisterOutput("test", newReporterFactory(outChan))
	monitor := initMonWithconfig(1, t)

	var maxEvents uint64 = 10
	queue := NewTestQueue(maxEvents)
	mon, err := NewFromConfig(monitor, queue)
	assert.NoError(t, err)
	mon.Watch()
	events := 0
	t.Logf("listening for events...")
	// once we have maxEvents, we can properly
	gotQueueIsFull := false
	var queueFullCount int

	for {
		evt := <-outChan
		events = events + 1

		if evt.QueueIsCurrentlyFull {
			gotQueueIsFull = true
		}

		queueFullCount = int(evt.QueueLimitReachedCount.ValueOr(0))

		if events == int(maxEvents) {
			t.Logf("Got %d events, exiting listener.", events)
			break
		}
	}

	assert.True(t, gotQueueIsFull, "Did not report a full queue")
	assert.NotZero(t, queueFullCount, "Got a queue full count of 0")
}

func TestQueueMetrics(t *testing.T) {
	var fullBytes uint64 = 91
	var fullLimitBytes uint64 = 100
	testEventBytes := queue.Metrics{
		ByteCount: opt.UintWith(fullBytes),
		ByteLimit: opt.UintWith(fullLimitBytes),
	}

	count, limit, isFull, err := getLimits(testEventBytes)
	assert.NoError(t, err)
	assert.Equal(t, count, fullBytes)
	assert.Equal(t, limit, fullLimitBytes)
	assert.True(t, isFull, true)

}
