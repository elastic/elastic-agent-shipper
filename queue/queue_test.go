package queue

import (
	"testing"

	"github.com/elastic/elastic-agent-shipper/api"
	"github.com/stretchr/testify/assert"
)

func TestSimpleBatch(t *testing.T) {
	queue, err := New()
	assert.NoError(t, err)
	defer queue.Close()

	eventCount := 100
	events := make([]api.Event, eventCount)
	for i := 0; i < eventCount; i++ {
		queue.Publish(&events[i])
	}

	// Confirm that all events made it through. We ignore the contents
	// since it's easier to just confirm the exact pointer values go
	// through unchanged.
	batch, err := queue.Get(eventCount)
	assert.NoError(t, err)

	assert.Equal(t, batch.Count(), eventCount)
	for i := 0; i < eventCount; i++ {
		event, ok := batch.Event(i).(*api.Event)
		assert.True(t, ok)
		// Need to use assert.True since assert.Equal* uses value comparison
		// for unequal pointers.
		assert.True(t, event == &events[i])
	}
}
