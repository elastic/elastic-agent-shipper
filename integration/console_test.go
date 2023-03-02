// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build integration

package integration

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/tools"
)

func TestServerStarts(t *testing.T) {
	path := t.TempDir()
	socket := tools.GenerateTestAddr(path)
	config := `
type: console
shipper:
  server:
    server: ` + socket + `
  output:
    console:
      enabled: true
`
	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "gRPC started")

	if !found {
		env.Fatalf("test executable failed to start")
	}
}

func TestServerFailsToStart(t *testing.T) {
	path := t.TempDir()
	socket := tools.GenerateTestAddr(path)
	config := `
type: console
shipper:
  server:
    server: ` + socket + `
  output:
    console:
      enabled: not_a_boolean
`
	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "error unpacking shipper config")

	if !found {
		env.Fatalf("didn't error on bad config")
	}
}

func TestPublishMessage(t *testing.T) {
	path := t.TempDir()
	socket := tools.GenerateTestAddr(path)
	config := `
type: console
shipper:
  server:
    server: ` + socket + `
  output:
    console:
      enabled: true
`
	env := NewTestingEnvironment(t, config)
	t.Cleanup(func() { env.Stop() })

	found := env.WaitUntil("stderr", "gRPC started")

	if !found {
		env.Fatalf("test executable failed to start")
	}

	found = env.Contains("stderr", "Initializing disk queue at path")
	if found {
		env.Fatalf("memory queue configured but disk queue started")
	}

	client := env.NewClient(socket)
	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	require.NoErrorf(t, err, "error creating events: %s\n", err)

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	require.NoErrorf(t, err, "Error publishing event: %s", err)

	found = env.WaitUntil("stdout", unique)

	if !found {
		env.Fatalf("event wasn't published")
	}
}

func TestPublishDiskQueue(t *testing.T) {
	path := t.TempDir()
	socket := tools.GenerateTestAddr(path)
	queue_path := filepath.Join(path, "queue")
	config := `
type: console
shipper:
  server:
    server: ` + socket + `
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

	found := env.WaitUntil("stderr", "gRPC started")

	if !found {
		env.Fatalf("test executable failed to start")
	}

	found = env.Contains("stderr", "Initializing disk queue at path")
	if !found {
		env.Fatalf("disk queue configured but not started")
	}

	client := env.NewClient(socket)
	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	require.NoErrorf(t, err, "error creating events: %s\n", err)

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	require.NoErrorf(t, err, "Error publishing event: %s", err)

	found = env.WaitUntil("stdout", unique)

	if !found {
		env.Fatalf("event wasn't published")
	}
}

func TestPubCompEncDiskQueue(t *testing.T) {
	path := t.TempDir()
	socket := tools.GenerateTestAddr(path)
	queue_path := filepath.Join(path, "queue")
	config := `
type: console
shipper:
  server:
    server: ` + socket + `
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

	found := env.WaitUntil("stderr", "gRPC started")

	if !found {
		env.Fatalf("test executable failed to start")
	}

	found = env.Contains("stderr", "Initializing disk queue at path")
	if !found {
		env.Fatalf("disk queue configured but not started")
	}

	client := env.NewClient(socket)
	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	require.NoErrorf(t, err, "error creating events: %s\n", err)

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	require.NoErrorf(t, err, "error publishing event: %s", err)

	found = env.WaitUntil("stdout", unique)

	if !found {
		env.Fatalf("event wasn't published")
	}
}
