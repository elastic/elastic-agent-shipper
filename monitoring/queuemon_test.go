// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package monitoring

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/beats/v7/libbeat/opt"
	"github.com/elastic/beats/v7/libbeat/publisher/queue"
	"github.com/elastic/elastic-agent-shipper/monitoring/reporter/expvar"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

//expvarQueue emulates what the expvar queue metrics look like on the other end
type expvarQueue struct {
	CurrentLevel int  `json:"current_level"`
	Maxlevel     int  `json:"max_level"`
	IsFull       bool `json:"is_full"`
	LimitCount   int  `json:"limit_reached_count"`
}

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

func getRandPort() int {
	return rand.Intn(65535-49152) + 49152 //nolint:gosec //This is a test, strong crypto not needed
}

// simple wrapper to return a generic config object
func initMonWithconfig(interval int, name string, port int) Config {
	return Config{
		Interval: time.Millisecond * time.Duration(interval),
		Enabled:  true,
		ExpvarOutput: expvar.Config{
			Enabled: true,
			Port:    port,
			Host:    "",
			Name:    name,
		},
	}
}

// fetch the expvar data from the http endpoint and return the final queue object to test the metrics outputs
func fetchExpVars(t *testing.T, client http.Client, endpoint string) expvarQueue {
	resp, err := client.Get(endpoint) //nolint:noctx //this is a test, with a timeout
	assert.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, resp.StatusCode, 200, "expvar endpoint did not return 200: %#v", resp)
	httpResp, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	raw := struct {
		Memstats runtime.MemStats
		Cmdline  []string
		Queue    expvarQueue
	}{}
	err = json.Unmarshal(httpResp, &raw)

	assert.NoError(t, err)

	return raw.Queue
}

// actual tests

func TestSetupMonitor(t *testing.T) {
	port := getRandPort()
	monitor := initMonWithconfig(1, "test", port)
	queue := NewTestQueue(10)
	mon, err := NewFromConfig(monitor, queue)
	assert.NoError(t, err)
	mon.Watch()
	mon.End()
}

func TestReportedEvents(t *testing.T) {
	port := getRandPort()
	monitor := initMonWithconfig(1, "queue", port)

	var maxEvents uint64 = 10
	queue := NewTestQueue(maxEvents)
	mon, err := NewFromConfig(monitor, queue)
	assert.NoError(t, err)
	mon.Watch()

	var limitCount int
	var queueFullCount int

	t.Logf("listening for events...")
	// once we have maxEvents, we can properly
	// use the expvar endpoint to check the output
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	endpoint := fmt.Sprintf("http://localhost:%d/debug/vars", port)

	// Sit and wait until we have interesting data we can test
	for {
		result := fetchExpVars(t, client, endpoint)
		t.Logf("Got raw result: %#v", result)
		if result.IsFull {
			queueFullCount = result.CurrentLevel
			limitCount = result.LimitCount

			break
		}

	}
	mon.End()

	assert.NotZero(t, limitCount, "Got a queue full count of 0")
	assert.Equal(t, int(maxEvents), queueFullCount)
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