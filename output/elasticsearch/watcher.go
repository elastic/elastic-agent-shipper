// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-transport-go/v8/elastictransport"
)

// WatchState represents the state of the output at the time the WatchReporter callback was made
type WatchState string

// WatchDegraded corresponds to a degraded state
var WatchDegraded WatchState = "degraded"

// WatchRecovered corresponds to a recovered state
var WatchRecovered WatchState = "recovered"

// WatchReporter is a callback that the watcher will call whenever the output has reached a degraded state, or recovered from a degraded state
type WatchReporter func(state WatchState, msg string)

// ESHealthWatcher monitors the ES connection and notifies if a failure persists for more than failureInterval seconds
type ESHealthWatcher struct {
	reporter          WatchReporter
	callbackFailCount int32
	failureInterval   time.Duration
	waitInterval      time.Duration
	esClient          ConnectionMetrics
	log               *logp.Logger
}

// ConnectionMetrics is an interface type for ES transport metrics
type ConnectionMetrics interface {
	Metrics() (elastictransport.Metrics, error)
}

//////////// Test helpers

// sets the ticker channel used by the watcher. Used for tests.
var monitoringTicker = createWatchTicker

// default ticker for the health monitor, can by bypassed for tests
func createWatchTicker(interval time.Duration) <-chan time.Time {
	return time.NewTicker(interval).C
}

// If set, is called every time the watcher loop returns to the start
// This helps us avoid copious sleep() statements in the tests
var watcherLoopResetChan chan struct{}

////////////

func newHealthWatcher(log *logp.Logger, reporter WatchReporter, failureInterval time.Duration, client ConnectionMetrics) ESHealthWatcher {
	return ESHealthWatcher{
		log:             log,
		reporter:        reporter,
		failureInterval: failureInterval,
		waitInterval:    time.Second * 5,
		esClient:        client,
	}
}

// Fail increments the count of callback failures
// This helps us determine health status in cases where errors originate outside of the ES transport. JSON errors, etc
func (hw *ESHealthWatcher) Fail() {
	atomic.AddInt32(&hw.callbackFailCount, 1)

}

// Watch starts a blocking loop and will report if there has been a failure for longer than a given period.
// This is a blocking call
func (hw *ESHealthWatcher) Watch(ctx context.Context) {

	// count of failures from the es transport
	lastTransportFail := 0
	// count of HTTP response codes in the fail range
	lastResponseFailRange := 0
	// count of HTTP response codes in the success range
	lastResponseSuccessfulRange := 0
	// is the watcher reported degraded?
	didFail := false
	// Count of last failures reported by the callback system
	var lastCBFail int32

	ticker := monitoringTicker(hw.failureInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker:
			metrics, err := hw.esClient.Metrics()
			if err != nil {
				hw.log.Errorf("error fetching metrics from ES cluster: %s", err)
				continue
			}

			// these return code ranges should probably be configurable?
			success := returnRangeDelta(200, 299, metrics.Responses)
			fails := returnRangeDelta(400, 599, metrics.Responses)

			// calculate deltas between last run
			respFailDelta := fails - lastResponseFailRange
			respSuccessDelta := success - lastResponseSuccessfulRange
			transportFailDelta := metrics.Failures - lastTransportFail
			cbFails := atomic.LoadInt32(&hw.callbackFailCount) - lastCBFail

			// the logic here is relatively simple
			// if there have been no successful HTTP codes since the last reporting period,
			// but we have gotten a non-zero change in failures, assume we're in a degraded state
			if respSuccessDelta == 0 {
				if cbFails > 0 || transportFailDelta > 0 || respFailDelta > 0 {
					hw.log.Debugf("failure deltas without successful events, marking as degraded; callback fail delta=%d; HTTP transport failures=%d; ES transport failures=%d", cbFails, transportFailDelta, respFailDelta)
					hw.reporter(WatchDegraded, "Degraded: no responses from ES")
					didFail = true
				}
			} else {
				hw.log.Debugf("since last monitoring period, watcher got %d successful return codes", respSuccessDelta)
			}

			// check to see if we're un-failed
			// Should there be a more complex success condition, other than "we saw some events successfully send"?
			if didFail && respSuccessDelta > 0 {
				hw.log.Debugf("got successful events, marking as recovered")
				hw.reporter(WatchRecovered, "ES is sending events")
				didFail = false
			}

			// update trackers
			lastTransportFail = metrics.Failures
			lastResponseFailRange = fails
			lastResponseSuccessfulRange = success
			lastCBFail = atomic.LoadInt32(&hw.callbackFailCount)

			// if we're in a test setting, send a signal telling the test
			// we're done running through the checks, and the state can be checked
			if watcherLoopResetChan != nil {
				hw.log.Debugf("watcher loop done, notifying test")
				watcherLoopResetChan <- struct{}{}
			}
		}

	}
}

// return the total sum
func returnRangeDelta(min, max int, returns map[int]int) int {
	acc := 0
	for code, val := range returns {
		if code >= min && code <= max {
			acc += val
		}
	}
	return acc
}
