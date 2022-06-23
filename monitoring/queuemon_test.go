// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package monitoring

import (
	"encoding/json"
	"expvar"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/elastic-agent-libs/opt"
	expvarReport "github.com/elastic/elastic-agent-shipper/monitoring/reporter/expvar"
	"github.com/elastic/elastic-agent-shipper/queue"
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

// Metrics spoofs the metrics output
func (tq *TestMetricsQueue) Metrics() (queue.Metrics, error) {
	tq.metricState.EventCount = opt.UintWith(tq.metricState.EventCount.ValueOr(0) + 1)

	if tq.metricState.EventCount.ValueOr(0) > tq.limit {
		tq.metricState.EventCount = opt.UintWith(0)
	}

	return tq.metricState, nil
}

// simple wrapper to return a generic config object
func initMonWithconfig(interval int, name string) Config {
	return Config{
		Interval: time.Millisecond * time.Duration(interval),
		Enabled:  true,
		ExpvarOutput: expvarReport.Config{
			Enabled: true,
			Port:    0,
			Host:    "",
			Name:    name,
		},
	}
}

// fetch the expvar data from the http endpoint and return the final queue object to test the metrics outputs
func fetchExpVars(client http.Client, endpoint string) (expvarQueue, error) {
	resp, err := client.Get(endpoint) //nolint:noctx //this is a test, with a timeout
	if err != nil {
		return expvarQueue{}, fmt.Errorf("error in HTTP get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return expvarQueue{}, fmt.Errorf("expvar endpoint did not return 200: %#v", resp)
	}
	httpResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return expvarQueue{}, fmt.Errorf("error reading response body from expvar endpoint: %w", err)
	}
	raw := struct {
		Memstats runtime.MemStats
		Cmdline  []string
		Queue    expvarQueue
	}{}
	err = json.Unmarshal(httpResp, &raw)
	if err != nil {
		return raw.Queue, fmt.Errorf("error unmarshalling json from expvar: %w", err)
	}

	return raw.Queue, nil
}

func startTestServer() string {
	// Start an http test server on a random port and re-register expvar's global endpoint
	// The only part of the monitoring library this bypasses is the "regular" http test server, as we're still
	// hitting all the code that we register via expvar
	ts := httptest.NewUnstartedServer(nil)
	ts.Config.Handler = expvar.Handler()
	ts.Start()
	return ts.URL
}

// actual tests

func TestSetupMonitor(t *testing.T) {
	monitor := initMonWithconfig(1, "test")
	queue := NewTestQueue(10)
	mon, err := NewFromConfig(monitor, queue)
	assert.NoError(t, err)
	mon.Watch()
	mon.End()
}

func TestReportedEvents(t *testing.T) {

	monitor := initMonWithconfig(1, "queue")

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

	//endpoint := fmt.Sprintf("http://localhost:%d/debug/vars", port)
	endpoint := startTestServer()
	t.Logf("Addr: %s", endpoint)
	// Sit and wait until we have interesting data we can test
	for {
		result, err := fetchExpVars(client, endpoint)
		if err != nil {
			t.Fatalf("Error fetching expvars: %s", err)
		}
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
