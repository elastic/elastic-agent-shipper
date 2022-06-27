// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package queue

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

func TestMemoryQueueSimpleBatch(t *testing.T) {
	cfg := DefaultConfig()
	queue, err := New(cfg)
	assert.NoError(t, err)
	defer queue.Close()

	eventCount := 100
	events := make([]messages.Event, eventCount)
	for i := 0; i < eventCount; i++ {
		err = queue.Publish(&events[i])
		assert.NoError(t, err, "couldn't publish to queue")
	}

	// Confirm that all events made it through. We ignore the contents
	// since it's easier to just confirm the exact pointer values go
	// through unchanged.
	batch, err := queue.Get(eventCount)
	assert.NoError(t, err, "couldn't get queue batch")

	assert.Equal(t, batch.Count(), eventCount)
	for i := 0; i < eventCount; i++ {
		event, ok := batch.Event(i).(*messages.Event)
		assert.True(t, ok, "queue output should have the same concrete type as its input")
		// Need to use assert.True since assert.Equal* uses value comparison
		// for unequal pointers.
		assert.True(t, event == &events[i], "memory queue should output the same pointer as its input")
	}
}

func TestQueueTypes(t *testing.T) {
	tests := map[string]struct {
		kind        string
		encryption  bool
		compression bool
	}{
		"memory":                      {kind: "memory"},
		"disk":                        {kind: "disk"},
		"disk_encryption":             {kind: "disk", encryption: true},
		"disk_compression":            {kind: "disk", compression: true},
		"disk_encryption_compression": {kind: "disk", encryption: true, compression: true},
	}
	for name, tc := range tests {
		cfg := DefaultConfig()
		cfg.Type = tc.kind
		if tc.kind == "disk" {
			dir, err := os.MkdirTemp("", t.Name()+"_"+name)
			assert.NoError(t, err, "couldn't make tempdir")
			defer os.RemoveAll(dir)
			cfg.DiskSettings.Path = dir
			cfg.DiskSettings.UseProtobuf = true
		}
		if tc.encryption {
			cfg.DiskSettings.EncryptionKey = []byte("testtesttesttest")
		}
		cfg.DiskSettings.UseCompression = tc.compression

		queue, err := New(cfg)
		assert.NoError(t, err)
		defer queue.Close()

		tracker := [10]bool{}
		for idx := range tracker {
			e := makeEvent(idx)
			err = queue.Publish(e)
			assert.NoError(t, err, "couldn't publish to queue")
		}

		got := 0
		for {
			batch, err := queue.Get(len(tracker))
			assert.NoError(t, err, "couldn't get queue batch")
			for i := 0; i < batch.Count(); i++ {
				//get each event and mark the index as received
				event, ok := batch.Event(i).(*messages.Event)
				assert.True(t, ok)
				d := event.GetFields().GetData()
				v := d["message"].GetNumberValue()
				tracker[int(v)] = true
			}
			got = got + batch.Count()
			if got == len(tracker) {
				break
			}
		}

		//make sure each index was had an event
		for _, val := range tracker {
			assert.True(t, val)
		}
	}
}

//makeEvent creates a sample event, with provided int as Fields.message
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
