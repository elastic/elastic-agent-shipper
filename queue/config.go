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
	MemSettings  *memqueue.Settings  `config:"memqueue"`
	DiskSettings *diskqueue.Settings `config:"diskqueue"`
}

func DefaultConfig() Config {
	return Config{
		MemSettings: &memqueue.Settings{
			Events:         1024,
			FlushMinEvents: 256,
			FlushTimeout:   5 * time.Millisecond,
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
