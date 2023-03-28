// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build !integration

package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/opt"
	"github.com/elastic/elastic-agent-shipper/queue"
	"github.com/elastic/elastic-agent-shipper/tools"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Shippermetrics struct {
	Queue queueMetrics `json:"queue"`
}

// queueMetrics emulates what the queue metrics look like on the other end
type queueMetrics struct {
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
func initMonWithconfig() (*config.C, string) {
	path := tools.GenerateTestAddr(os.TempDir()) //filepath.Join(os.TempDir(), fmt.Sprintf("mon%d.sock", time.Now().Unix()))
	fmt.Printf("got path: %s\n", path)
	return config.MustNewConfigFrom(map[string]interface{}{
		"enabled": true,
		"host":    path, //fmt.Sprintf("unix://%s", path),
		"port":    "8182",
		"timeout": "4s"}), path
}

// actual tests

func TestSetupMonitor(t *testing.T) {
	monitor, path := initMonWithconfig()
	defer os.Remove(path)
	queue := NewTestQueue(10)
	mon, err := NewFromConfig(monitor, nil, queue)
	assert.NoError(t, err)
	mon.End()
}

func TestFetchInfo(t *testing.T) {

	var maxEvents uint64 = 10
	client, mon, path := createMetricsClientServer(t, maxEvents)
	defer os.Remove(path)

	endpoint := fmt.Sprintf("http://unix%s", "/")

	rawInfo := fetchEndpointData(t, client, endpoint)

	parsed := map[string]interface{}{}

	err := json.Unmarshal(rawInfo, &parsed)
	require.NoError(t, err)

	t.Logf("got: %#v", parsed)

	// Just make sure that we put the values in the correct place, and they exist
	// ephemeral_id is random.
	_, ok := parsed["ephemeral_id"]
	require.True(t, ok, "missing ephemeral_id")
	_, ok = parsed["binary_arch"]
	require.True(t, ok, "missing binary_arch")
	_, ok = parsed["uid"]
	require.True(t, ok, "missing uid")

	mon.End()
}

func TestReportedEvents(t *testing.T) {
	var limitCount int
	var queueFullCount int
	var maxEvents uint64 = 10

	client, mon, path := createMetricsClientServer(t, maxEvents)
	defer os.Remove(path)

	endpoint := fmt.Sprintf("http://unix%s", "/shipper")

	t.Logf("Addr: %s", path)
	// Sit and wait until the queue fills up
	for {
		result := fetchEndpointData(t, client, endpoint)

		raw := struct {
			Shipper Shippermetrics
		}{}
		err := json.Unmarshal(result, &raw)
		require.NoError(t, err)

		if raw.Shipper.Queue.IsFull {
			queueFullCount = raw.Shipper.Queue.CurrentLevel
			limitCount = raw.Shipper.Queue.LimitCount
			t.Logf("Got final result: %#v", raw.Shipper.Queue)
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

func createMetricsClientServer(t *testing.T, maxEvents uint64) (http.Client, *QueueMonitor, string) {
	_ = logp.DevelopmentSetup()
	monitor, path := initMonWithconfig()
	queue := NewTestQueue(maxEvents)
	mon, err := NewFromConfig(monitor, nil, queue)
	require.NoError(t, err)

	client := http.Client{
		Timeout: time.Second * 4,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return tools.DialTestAddr(path)
			},
		},
	}

	return client, mon, path
}

// fetch the expvar data from the http endpoint and return the final queue object to test the metrics outputs
func fetchEndpointData(t *testing.T, client http.Client, endpoint string) []byte {
	resp, err := client.Get(endpoint) //nolint:noctx //this is a test, with a timeout
	require.NoError(t, err, "error in HTTP get")

	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode, "expvar endpoint did not return 200")

	httpResp, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "error reading response body from expvar endpoint")

	return httpResp

}
