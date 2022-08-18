// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package queue

import (
	"fmt"
	"time"

	diskqueue "github.com/elastic/beats/v7/libbeat/publisher/queue/diskqueue"
	memqueue "github.com/elastic/beats/v7/libbeat/publisher/queue/memqueue"
)

type Config struct {
	MemSettings  *memqueue.Settings  `config:"mem"`
	DiskSettings *diskqueue.Settings `config:"disk"`
}

func DefaultConfig() Config {
	// Use the same default memory queue configuration that Beats does:
	// https://github.com/elastic/beats/blob/7449e5c4b944c661299de8099d5423bafd458ee2/libbeat/publisher/queue/memqueue/config.go#L32
	return Config{
		MemSettings: &memqueue.Settings{
			Events:         4096,
			FlushMinEvents: 2048,
			FlushTimeout:   1 * time.Second,
		}, //memqueue should have a DefaultSettings()
		DiskSettings: nil,
	}
}

func (c *Config) Validate() error {
	if c.MemSettings == nil && c.DiskSettings == nil {
		return fmt.Errorf("memory or disk queue settings must be supplied")
	}
	return nil
}

func (c Config) useDiskQueue() bool {
	return c.DiskSettings != nil
}
