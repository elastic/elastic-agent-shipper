// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package queue

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

func TestSimpleBatch(t *testing.T) {
	queue, err := New()
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
