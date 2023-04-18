// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-transport-go/v8/elastictransport"
)

type testESConn struct {
	data elastictransport.Metrics
}

func (tc *testESConn) Metrics() (elastictransport.Metrics, error) {
	return tc.data, nil
}

func setupTestEnv(reporter WatchReporter) (chan time.Time, *testESConn, ESHealthWatcher, chan struct{}) {
	_ = logp.DevelopmentSetup()
	transport := &testESConn{data: elastictransport.Metrics{
		Responses: map[int]int{},
	}}

	testTimeTicker := make(chan time.Time)

	monitoringTicker = func(_ time.Duration) <-chan time.Time {
		return testTimeTicker
	}

	watcherLoopResetChan = make(chan struct{})

	watcher := newHealthWatcher(logp.L(), reporter, time.Second, transport)

	return testTimeTicker, transport, watcher, watcherLoopResetChan
}

func TestUnreachableServer(t *testing.T) {
	gotFail := false
	reportFail := func(state WatchState, s string) {
		t.Logf("got: %s", s)
		if state == WatchDegraded {
			gotFail = true
		}

	}
	testTimeTicker, transport, watcher, watcherLoopResetChan := setupTestEnv(reportFail)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// add a time so we get one initial loop
	go watcher.Watch(ctx)
	testTimeTicker <- time.Now()
	<-watcherLoopResetChan

	// emulate failures
	transport.data.Failures = 10
	testTimeTicker <- time.Now()

	<-watcherLoopResetChan
	require.True(t, gotFail)

}

func TestFailedReturnCodes(t *testing.T) {
	gotFail := false
	reportFail := func(state WatchState, s string) {
		t.Logf("got: %s", s)
		if state == WatchDegraded {
			gotFail = true
		}

	}
	testTimeTicker, transport, watcher, resetChan := setupTestEnv(reportFail)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	go watcher.Watch(ctx)
	testTimeTicker <- time.Now()
	<-resetChan
	// emulate failures
	transport.data.Responses[500] = 10
	testTimeTicker <- time.Now()
	<-resetChan
	require.True(t, gotFail)
}

func TestMixedReturnCodes(t *testing.T) {
	gotFail := false
	reportFail := func(state WatchState, s string) {
		t.Logf("got: %s", s)
		if state == WatchDegraded {
			gotFail = true
		}

	}
	testTimeTicker, transport, watcher, resetChan := setupTestEnv(reportFail)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	go watcher.Watch(ctx)
	testTimeTicker <- time.Now()
	<-resetChan

	// mix failed events with successful events
	transport.data.Responses[500] = 10
	transport.data.Responses[200] = 300
	testTimeTicker <- time.Now()

	<-resetChan
	// should not mark as unhealthy
	require.False(t, gotFail)
}

func TestSuccessfulThenUnhealthy(t *testing.T) {
	gotFail := false
	reportFail := func(state WatchState, s string) {
		t.Logf("got: %s", s)
		if state == WatchDegraded {
			gotFail = true
		}

	}
	testTimeTicker, transport, watcher, resetChan := setupTestEnv(reportFail)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	go watcher.Watch(ctx)
	// generate a mix of fail and successful return codes
	transport.data.Responses[500] = 10
	transport.data.Responses[200] = 300
	testTimeTicker <- time.Now()
	<-resetChan

	//another period with just failures
	transport.data.Responses[500] = 20
	testTimeTicker <- time.Now()
	<-resetChan

	require.True(t, gotFail)
}

func TestCallbackFailures(t *testing.T) {
	gotFail := false
	reportFail := func(state WatchState, s string) {
		t.Logf("got: %s", s)
		if state == WatchDegraded {
			gotFail = true
		}

	}
	testTimeTicker, transport, watcher, resetChan := setupTestEnv(reportFail)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// start with initial data
	go watcher.Watch(ctx)
	transport.data.Responses[500] = 10
	transport.data.Responses[200] = 300
	testTimeTicker <- time.Now()
	<-resetChan
	// failures specified on callback
	watcher.Fail()
	watcher.Fail()

	testTimeTicker <- time.Now()
	<-resetChan

	require.True(t, gotFail)
}

func TestFailureWithRecovery(t *testing.T) {
	gotFail := false
	reportFail := func(state WatchState, s string) {
		t.Logf("got: %s", s)
		if state == WatchDegraded {
			gotFail = true
		}
		if state == WatchRecovered {
			gotFail = false
		}
	}
	testTimeTicker, transport, watcher, resetChan := setupTestEnv(reportFail)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// start with a basic mix of failures and success
	go watcher.Watch(ctx)
	transport.data.Responses[500] = 10
	transport.data.Responses[200] = 300
	testTimeTicker <- time.Now()
	<-resetChan
	require.False(t, gotFail)

	// degraded state
	transport.data.Responses[400] = 10
	testTimeTicker <- time.Now()
	<-resetChan
	require.True(t, gotFail)

	// now recover
	transport.data.Responses[200] = 350
	testTimeTicker <- time.Now()
	<-resetChan
	require.False(t, gotFail)
}
