// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build integration

package integration

import (
	"testing"

	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/stretchr/testify/require"
)

func TestServerStarts(t *testing.T) {
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
  console:
    enabled: true
`
	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "gRPC server is ready and is listening on")

	if !found {
		env.Fatalf("Test executable failed to start.")
	}
}

func TestServerFailsToStart(t *testing.T) {
	config := `
server:
  strict_mode: false
  port: 50052
  tls: not_boolean
logging:
  level: debug
  selectors: ["*"]
  to_stderr: true
output:
  console:
    enabled: true
`

	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "error unpacking shipper config")

	if !found {
		env.Fatalf("didn't error on bad config")
	}
}

func TestPublishMessage(t *testing.T) {
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
  console:
    enabled: true
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

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	require.NoErrorf(t, err, "Error publishing event: %s", err)

	found = env.WaitUntil("stdout", unique)

	if !found {
		env.Fatalf("Event wasn't published")
	}
}

func TestPublishDiskQueue(t *testing.T) {
	queue_path := t.TempDir()

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
  console:
    enabled: true
queue:
  disk:
    path: ` + queue_path + `
    max_size: 10G
`

	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "gRPC server is ready and is listening on")

	if !found {
		env.Fatalf("Test executable failed to start")
	}

	found = env.Contains("stderr", "Initializing disk queue at path")
	if !found {
		env.Fatalf("Disk queue configured but not started")
	}

	client := env.NewClient("localhost:50052")
	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	require.NoErrorf(t, err, "error creating events: %s\n", err)

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	require.NoErrorf(t, err, "Error publishing event: %s", err)

	found = env.WaitUntil("stdout", unique)

	if !found {
		env.Fatalf("Event wasn't published")
	}
}

func TestPublishCompressEncryptedDiskQueue(t *testing.T) {
	queue_path := t.TempDir()

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
  console:
    enabled: true
queue:
  disk:
    path: ` + queue_path + `
    max_size: 10G
    use_compression: true
    encryption_password: secret
`

	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "gRPC server is ready and is listening on")

	if !found {
		env.Fatalf("Test executable failed to start")
	}

	found = env.Contains("stderr", "Initializing disk queue at path")
	if !found {
		env.Fatalf("Disk queue configured but not started")
	}

	client := env.NewClient("localhost:50052")
	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	require.NoErrorf(t, err, "error creating events: %s\n", err)

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	require.NoErrorf(t, err, "error publishing event: %s", err)

	found = env.WaitUntil("stdout", unique)

	if !found {
		env.Fatalf("Event wasn't published")
	}
}
