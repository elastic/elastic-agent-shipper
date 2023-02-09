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
	cfg := config.MustNewConfigFrom(`
api_key: "test"
username: "user"`)
	_, err := readConfig(cfg)
	if err == nil {
		t.Fatalf("Valid configurations can't include both api key and username")
	}
}

func TestWatcherPeriodWithNoEvents(t *testing.T) {
	report := func(s string) {
		t.Logf("got: %s, should not have failed", s)
		t.Fail()
	}
	watcher := ESHeathWatcher{failureInterval: time.Second, reportFail: report, waitInterval: time.Millisecond * 100}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	go watcher.Watch(ctx)
	watcher.Success()
	// should not report failure if it's just been a long period without any events
	time.Sleep(time.Second * 1)
}

func TestWatcherWithFail(t *testing.T) {
	gotFail := false
	reportFail := func(s string) {
		t.Logf("got: %s", s)
		gotFail = true
	}
	watcher := ESHeathWatcher{failureInterval: time.Millisecond * 400, reportFail: reportFail, waitInterval: time.Millisecond * 100}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	go watcher.Watch(ctx)

	watcher.Success()
	time.Sleep(time.Millisecond * 100)
	watcher.Fail()
	// should fail
	time.Sleep(time.Millisecond * 600)
	assert.True(t, gotFail, "watcher should report a failure")
}

func TestWatcherWithFailAndSuccess(t *testing.T) {
	gotFail := false
	gotSuccess := false
	reportFail := func(s string) {
		t.Logf("got: %s", s)
		gotFail = true
	}
	reportSuccess := func(s string) {
		t.Logf("got: %s", s)
		gotSuccess = true
	}

	watcher := ESHeathWatcher{failureInterval: time.Millisecond * 400, reportFail: reportFail, reportHealthy: reportSuccess, waitInterval: time.Millisecond * 100}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	go watcher.Watch(ctx)

	watcher.Success()
	time.Sleep(time.Millisecond * 100)
	watcher.Fail()
	// should fail
	time.Sleep(time.Millisecond * 600)
	// report success
	watcher.Success()
	time.Sleep(time.Millisecond * 500)

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
