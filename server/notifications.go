// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"sync"
	"sync/atomic"
)

// state is an extendable struct that stores
// the information subscribers need to be notified about
type state struct {
	persistedIndex uint64
}

// Equals returns true if 2 instances of the state are equal
func (s *state) Equals(v *state) bool {
	return (s == nil && v == nil) || (s != nil && v != nil && s.persistedIndex == v.persistedIndex)
}

type notifications struct {
	mutex *sync.RWMutex
	cond  *sync.Cond

	// the whole state must be always replaced with `notify`
	// the struct fields are not thread safe on their own
	state *state

	// the notifications are stopped when this flag > 0
	stopped uint32
}

// getState return the current state or `nil` if the initial state
// is not set or if the notifications are stopped.
// This is thread-safe.
func (n *notifications) getState() *state {
	n.mutex.RLock()
	defer n.mutex.RUnlock()
	return n.state
}

// wait returns a pointer to the current state or blocks until the
// first state change or when notifications are stopped.
func (n *notifications) wait() *state {
	// before trying to lock we check the flag
	// it should be less expensive
	if atomic.LoadUint32(&n.stopped) != 0 {
		return nil
	}

	n.cond.L.Lock()
	defer n.cond.L.Unlock()

	// waiting until there is an initial state or everything is stopped
	for n.state == nil && atomic.LoadUint32(&n.stopped) == 0 {
		n.cond.Wait()
	}

	return n.state
}

// notify notifies all the subscribers and returns `true`
// or does not notify and returns `false` in case there is no update in the state
func (n *notifications) notify(value state) bool {
	if atomic.LoadUint32(&n.stopped) != 0 {
		return false
	}
	n.cond.L.Lock()
	defer n.cond.L.Unlock()
	if value.Equals(n.state) {
		return false
	}
	n.state = &value
	n.cond.Broadcast()

	return true
}

// shutdown stops waiting for all the subscribers and sets the current state to `nil`.
func (n *notifications) shutdown() {
	if !atomic.CompareAndSwapUint32(&n.stopped, 0, 1) {
		return
	}
	n.cond.L.Lock()
	defer n.cond.L.Unlock()
	n.state = nil
	// trigger unblock on each `Wait` so the subscribers could check the `stopped` flag and stop waiting
	n.cond.Broadcast()
}
