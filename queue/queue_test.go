// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build !integration

package queue

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/elastic/beats/v7/libbeat/publisher/queue/memqueue"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

func TestMemoryQueueSimpleBatch(t *testing.T) {
	cfg := DefaultConfig()
	queue, err := New(cfg)
	assert.NoError(t, err)
	defer queue.Close()

	eventCount := 100
	events := make([]messages.Event, eventCount)
	for i := 0; i < eventCount; i++ {
		_, err = queue.Publish(context.Background(), &events[i])
		assert.NoError(t, err, "couldn't publish to queue")
	}

	// Confirm that all events made it through. We ignore the contents
	// since it's easier to just confirm the exact pointer values go
	// through unchanged.
	batch, err := queue.Get(eventCount)
	assert.NoError(t, err, "couldn't get queue batch")

	returned := batch.Events()
	require.Equal(t, len(returned), eventCount)
	for i := 0; i < eventCount; i++ {
		// Need to use assert.True since assert.Equal* uses value comparison
		// for unequal pointers.
		assert.True(t, returned[i] == &events[i], "memory queue should output the same pointer as its input")
	}
}

func TestQueueTypes(t *testing.T) {
	tests := map[string]struct {
		memSettings *memqueue.Settings
		diskConfig  *DiskConfig
	}{
		"memory": {
			memSettings: &memqueue.Settings{
				Events:         1024,
				FlushMinEvents: 256,
				FlushTimeout:   5 * time.Millisecond,
			},
		},
		"disk no mem": {
			diskConfig: &DiskConfig{
				MaxSize: 10 * 1024 * 1024,
			},
		},
		"disk with mem": {
			memSettings: &memqueue.Settings{
				Events:         1024,
				FlushMinEvents: 256,
				FlushTimeout:   5 * time.Millisecond,
			},
			diskConfig: &DiskConfig{
				MaxSize: 10 * 1024 * 1024,
			},
		},
		"disk_encryption": {
			diskConfig: &DiskConfig{
				MaxSize:            10 * 1024 * 1024,
				EncryptionPassword: "testtesttesttest",
			},
		},
		"disk_compression": {
			diskConfig: &DiskConfig{
				MaxSize:        10 * 1024 * 1024,
				UseCompression: true,
			},
		},
		"disk_encryption_compression": {
			diskConfig: &DiskConfig{
				MaxSize:            10 * 1024 * 1024,
				UseCompression:     true,
				EncryptionPassword: "testtesttesttest",
			},
		},
	}
	for name, tc := range tests {
		cfg := DefaultConfig()
		cfg.MemSettings = tc.memSettings
		cfg.DiskConfig = tc.diskConfig
		if cfg.DiskConfig != nil {
			dir, err := os.MkdirTemp("", t.Name()+"_"+name)
			assert.NoError(t, err, "couldn't make tempdir")
			defer os.RemoveAll(dir)
			cfg.DiskConfig.Path = dir
		}

		queue, err := New(cfg)
		assert.NoError(t, err)
		defer queue.Close()

		tracker := [10]bool{}
		for idx := range tracker {
			e := makeEvent(idx)
			_, err = queue.Publish(context.Background(), e)
			assert.NoError(t, err, "couldn't publish to queue")
		}

		got := 0
		for {
			batch, err := queue.Get(len(tracker))
			assert.NoError(t, err, "couldn't get queue batch")
			for i := 0; i < batch.Count(); i++ {
				// get each event and mark the index as received
				event, ok := batch.Entry(i).(*messages.Event)
				require.True(t, ok)
				data := event.GetFields().GetData()
				testField, prs := data["message"]
				assert.True(t, prs)
				v := testField.GetNumberValue()
				tracker[int(v)] = true
			}
			got = got + batch.Count()
			if got == len(tracker) {
				break
			}
		}

		// make sure each index was had an event
		for _, val := range tracker {
			assert.True(t, val)
		}
	}
}

// makeEvent creates a sample event, with provided int as Fields.message
func makeEvent(msg int) *messages.Event {
	return &messages.Event{
		Timestamp: timestamppb.Now(),
		Fields: &messages.Struct{
			Data: map[string]*messages.Value{
				"message": {
					Kind: &messages.Value_NumberValue{
						NumberValue: float64(msg),
					},
				},
			},
		},
	}
}
