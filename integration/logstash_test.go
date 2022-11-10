// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/stretchr/testify/require"
)

type LogstashEventStats struct {
	Events struct {
		In  int
		Out int
	}
}

func TestLogstashOutputServerStarts(t *testing.T) {
	config := `
server:
  strict_mode: false
  port: 50052
  tls: false
logging:
  level: debug
  selectors: ["*"]
  to_stderr: true
output:
  logstash:
    enabled: true
    timeout: 1s
    hosts: ['127.0.0.1:5044']
`
	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "gRPC server is ready and is listening on")

	if !found {
		env.Fatalf("Test executable failed to start.")
	}
}

func TestLogstashOutputPublishMessage(t *testing.T) {
	config := `
server:
  strict_mode: false
  port: 50052
  tls: false
logging:
  level: debug
  selectors: ["*"]
  to_stderr: true
output:
  logstash:
    enabled: true
    timeout: 1s
    hosts: ['127.0.0.1:5044']
`

	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "gRPC server is ready and is listening on")

	if !found {
		env.Fatalf("Test executable failed to start")
	}

	found = env.Contains("stderr", "Initializing disk queue at path")
	if found {
		env.Fatalf("Memory queue configured but disk queue started")
	}

	client := env.NewClient("localhost:50052")
	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	require.NoErrorf(t, err, "error creating events: %s\n", err)

	preStats, err := getEventStats()
	require.NoErrorf(t, err, "Error getting Logstash stats before publish: %s", err)

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	require.NoErrorf(t, err, "Error publishing event: %s", err)

	found = env.WaitUntil("stderr", "finished publishing a batch")
	if !found {
		env.Fatalf("Event wasn't published")
	}
	found = env.WaitUntil("stderr", "ackloop: return")
	if !found {
		env.Fatalf("Event wasn't acked")
	}
	postStats, err := getEventStats()
	require.NoErrorf(t, err, "Error getting Logstash stats after publish: %s", err)
	require.Equalf(t, postStats.Events.In, preStats.Events.In+1, "post.in was %d, pre.in was %d", postStats.Events.In, preStats.Events.In)
	require.Equalf(t, postStats.Events.Out, preStats.Events.Out+1, "post.out was %d, pre.out was %d", postStats.Events.Out, preStats.Events.Out)
}

func TestLogstashOutputFailOneOutput(t *testing.T) {
	config := `
server:
  strict_mode: false
  port: 50052
  tls: false
logging:
  level: debug
  selectors: ["*"]
  to_stderr: true
output:
  logstash:
    enabled: true
    timeout: 1s
    hosts: ['127.0.0.2:5044', '127.0.0.1:5044']
`

	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "gRPC server is ready and is listening on")

	if !found {
		env.Fatalf("Test executable failed to start")
	}

	found = env.Contains("stderr", "Initializing disk queue at path")
	if found {
		env.Fatalf("Memory queue configured but disk queue started")
	}

	client := env.NewClient("localhost:50052")
	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	require.NoErrorf(t, err, "error creating events: %s\n", err)

	preStats, err := getEventStats()
	require.NoErrorf(t, err, "Error getting Logstash stats before publish: %s", err)

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	require.NoErrorf(t, err, "Error publishing event: %s", err)

	found = env.WaitUntil("stderr", "finished publishing a batch")
	if !found {
		env.Fatalf("Event wasn't published")
	}

	found = env.WaitUntil("stderr", "error connecting to 127.0.0.2:5044")
	if !found {
		env.Fatalf("first host didn't fail")
	}

	found = env.WaitUntil("stderr", "ackloop: return")
	if !found {
		env.Fatalf("Event wasn't acked")
	}
	postStats, err := getEventStats()
	require.NoErrorf(t, err, "Error getting Logstash stats after publish: %s", err)
	require.Equalf(t, postStats.Events.In, preStats.Events.In+1, "post.in was %d, pre.in was %d", postStats.Events.In, preStats.Events.In)
	require.Equalf(t, postStats.Events.Out, preStats.Events.Out+1, "post.out was %d, pre.out was %d", postStats.Events.Out, preStats.Events.Out)
}

func TestLogstashOutputReconnect(t *testing.T) {
	config := `
server:
  strict_mode: false
  port: 50052
  tls: false
logging:
  level: debug
  selectors: ["*"]
  to_stderr: true
output:
  logstash:
    enabled: true
    timeout: 1s
    ttl: 1s
    hosts: ['127.0.0.1:5044']
`

	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "gRPC server is ready and is listening on")

	if !found {
		env.Fatalf("Test executable failed to start")
	}

	found = env.Contains("stderr", "Initializing disk queue at path")
	if found {
		env.Fatalf("Memory queue configured but disk queue started")
	}

	client := env.NewClient("localhost:50052")
	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	require.NoErrorf(t, err, "error creating events: %s\n", err)

	preStats, err := getEventStats()
	require.NoErrorf(t, err, "Error getting Logstash stats before publish: %s", err)

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	require.NoErrorf(t, err, "Error publishing event: %s", err)

	found = env.WaitUntil("stderr", "finished publishing a batch")
	if !found {
		env.Fatalf("Event wasn't published")
	}

	found = env.WaitUntil("stderr", "ackloop: return")
	if !found {
		env.Fatalf("Event wasn't acked")
	}
	postStats, err := getEventStats()
	require.NoErrorf(t, err, "Error getting Logstash stats after publish: %s", err)
	require.Equalf(t, postStats.Events.In, preStats.Events.In+1, "post.in was %d, pre.in was %d", postStats.Events.In, preStats.Events.In)
	require.Equalf(t, postStats.Events.Out, preStats.Events.Out+1, "post.out was %d, pre.out was %d", postStats.Events.Out, preStats.Events.Out)

	time.Sleep(1 * time.Second)

	unique = "UniqueStringToLookForInOutput2"
	events, err = createEvents([]string{unique})
	require.NoErrorf(t, err, "error creating events: %s\n", err)
	preStats, err = getEventStats()
	require.NoErrorf(t, err, "Error getting Logstash stats before publish: %s", err)

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	require.NoErrorf(t, err, "Error publishing event: %s", err)

	found = env.WaitUntil("stderr", "finished publishing a batch")
	if !found {
		env.Fatalf("Event wasn't published")
	}

	found = env.WaitUntil("stderr", "ttl expired, reconnecting to logstash host")
	if !found {
		env.Fatalf("TTL expire didn't happen")
	}

	found = env.WaitUntil("stderr", "ackloop: return")
	if !found {
		env.Fatalf("Event wasn't acked")
	}
	postStats, err = getEventStats()
	require.NoErrorf(t, err, "Error getting Logstash stats after publish: %s", err)
	require.Equalf(t, postStats.Events.In, preStats.Events.In+1, "post.in was %d, pre.in was %d", postStats.Events.In, preStats.Events.In)
	require.Equalf(t, postStats.Events.Out, preStats.Events.Out+1, "post.out was %d, pre.out was %d", postStats.Events.Out, preStats.Events.Out)
}

func getEventStats() (LogstashEventStats, error) {
	events := LogstashEventStats{}
	res, err := http.Get("http://localhost:9600/_node/stats/events")
	if err != nil {
		return events, fmt.Errorf("error getting logstash event stats: %w", err)
	}
	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return events, fmt.Errorf("could not read logstash event stats response: %w", err)
	}
	err = json.Unmarshal(resBody, &events)
	if err != nil {
		return events, fmt.Errorf("cound not unmarshal logstash event stats response: %w", err)
	}
	return events, nil
}
