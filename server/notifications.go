// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"context"
	"sync"

	"github.com/gofrs/uuid"
)

const (
	// how many notifications can be in a notification buffer.
	notificationBufferSize = 16
)

type change struct {
	// persistedIndex is a changed persisted index or `nil` if the value has not changed.
	persistedIndex *uint64
}

// Any returns true if there is a change.
func (c change) Any() bool {
	return c.persistedIndex != nil
}

type notifications struct {
	subscribers map[uuid.UUID]chan change
	mutex       *sync.Mutex
}

func (n *notifications) subscribe() (out <-chan change, stop func(), err error) {
	ch := make(chan change, notificationBufferSize)
	id, err := uuid.NewV4()
	if err != nil {
		return nil, nil, err
	}

	n.mutex.Lock()
	n.subscribers[id] = ch
	n.mutex.Unlock()

	stop = func() {
		n.mutex.Lock()
		// we must block till the end of the function so `shutdown`
		// does not try to close closed channels and panics
		defer n.mutex.Unlock()

		// `shutdown` could shut down the notifications before this is executed
		_, ok := n.subscribers[id]
		if ok {
			delete(n.subscribers, id)
			close(ch)
			// drain the channel's buffer
			for range ch {
			}
		}
	}

	return ch, stop, nil
}

// notify notifies all the subscribers.
// This function should be used asynchronously.
// If one of subscriber channel's buffer is exhausted it might block
// until the subscriber reads the previous notification or unsubscribes.
func (n *notifications) notify(ctx context.Context, value change) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	for _, ch := range n.subscribers {
		select {
		case <-ctx.Done():
			return
		case ch <- value:
			continue
		}
	}
}

// shutdown closes all the subscriber channels and drains them.
func (n *notifications) shutdown() {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	for _, ch := range n.subscribers {
		close(ch)
		// drain the channel buffer
		for range ch {
		}
	}
	n.subscribers = make(map[uuid.UUID]chan change)
}
