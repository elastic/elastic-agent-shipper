// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/elastic-agent-libs/config"
)

func TestValidConfig(t *testing.T) {
	cfg := config.MustNewConfigFrom("enabled: true")
	result, err := readConfig(cfg)
	if err != nil {
		t.Fatalf("Can't create test configuration from valid input")
	}
	assert.Equal(t, *result, Config{Enabled: true})
}

func TestInvalidConfig(t *testing.T) {
	// TODO: Add a real test case here when we do enough config validation
	// that an invalid outcome is possible.
}

func TestWatcherPeriodWithNoEvents(t *testing.T) {
	report := func(state WatchState, s string) {
		if state == WatchDegraded {
			t.Logf("got: %s, should not have failed", s)
			t.Fail()
		}
	}
	watcher := ESHealthWatcher{failureInterval: time.Millisecond * 10, reporter: report, waitInterval: time.Millisecond * 5}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	go watcher.Watch(ctx)
	watcher.Success()
	// should not report failure if it's just been a long period without any events
	time.Sleep(time.Millisecond * 100)
}

func TestWatcherWithFail(t *testing.T) {
	gotFail := false
	reportFail := func(state WatchState, s string) {
		t.Logf("got: %s", s)
		if state == WatchDegraded {
			gotFail = true
		}

	}
	watcher := ESHealthWatcher{failureInterval: time.Millisecond * 10, reporter: reportFail, waitInterval: time.Millisecond * 5}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	go watcher.Watch(ctx)

	watcher.Success()
	time.Sleep(time.Millisecond * 10)
	watcher.Fail("simulated failure")
	// should fail
	time.Sleep(time.Millisecond * 15)
	assert.True(t, gotFail, "watcher should report a failure")
}

func TestWatcherWithFailAndSuccess(t *testing.T) {
	gotFail := false
	gotSuccess := false
	reportMethod := func(state WatchState, s string) {
		t.Logf("got: %s", s)
		if state == WatchDegraded {
			gotFail = true
		}
		if state == WatchRecovered {
			gotSuccess = true
		}
	}

	watcher := ESHealthWatcher{failureInterval: time.Millisecond * 10, reporter: reportMethod, waitInterval: time.Millisecond * 2}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	go watcher.Watch(ctx)

	watcher.Success()
	time.Sleep(time.Millisecond * 10)
	watcher.Fail("simulated failure")
	// should fail
	time.Sleep(time.Millisecond * 15)
	// report success
	watcher.Success()
	time.Sleep(time.Millisecond * 15)

	assert.True(t, gotFail, "watcher should report a failure")
	assert.True(t, gotSuccess, "watcher should report a healthy status")

}

func readConfig(cfg *config.C) (*Config, error) {
	c := Config{}
	if err := cfg.Unpack(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
