// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package queue

import (
	"crypto/sha1"
	"fmt"
	"time"

	"golang.org/x/crypto/pbkdf2"

	"github.com/elastic/beats/v7/libbeat/common/cfgtype"
	diskqueue "github.com/elastic/beats/v7/libbeat/publisher/queue/diskqueue"
	memqueue "github.com/elastic/beats/v7/libbeat/publisher/queue/memqueue"
)

type Config struct {
	MemSettings *memqueue.Settings `config:"mem"`
	DiskConfig  *DiskConfig        `config:"disk"`
}

func DefaultConfig() Config {
	// Use the same default memory queue configuration that Beats does:
	// https://github.com/elastic/beats/blob/7449e5c4b944c661299de8099d5423bafd458ee2/libbeat/publisher/queue/memqueue/config.go#L32
	return Config{
		MemSettings: &memqueue.Settings{
			Events:         4096,
			FlushMinEvents: 2048,
			FlushTimeout:   1 * time.Second,
		}, // memqueue should have a DefaultSettings()
		DiskConfig: nil,
	}
}

func (c *Config) Validate() error {
	if c.MemSettings == nil && c.DiskConfig == nil {
		return fmt.Errorf("memory or disk queue settings must be supplied")
	}
	return nil
}

func (c Config) useDiskQueue() bool {
	return c.DiskConfig != nil
}

type DiskConfig struct {
	Path               string            `config:"path"`
	MaxSize            cfgtype.ByteSize  `config:"max_size" validate:"required"`
	SegmentSize        *cfgtype.ByteSize `config:"segment_size"`
	ReadAheadLimit     *int              `config:"read_ahead"`
	WriteAheadLimit    *int              `config:"write_ahead"`
	RetryInterval      *time.Duration    `config:"retry_interval" validate:"positive"`
	MaxRetryInterval   *time.Duration    `config:"max_retry_interval" validate:"positive"`
	EncryptionPassword string            `config:"encryption_password"`
	UseCompression     bool              `config:"use_compression"`
}

func (c *DiskConfig) Validate() error {
	// If the segment size is explicitly specified, the total queue size must
	// be at least twice as large.
	if c.SegmentSize != nil && c.MaxSize != 0 && c.MaxSize < *c.SegmentSize*2 {
		return fmt.Errorf("disk queue max_size must be at least twice as big as segment_size")
	}

	// We require a total queue size of at least 10MB, and a segment size of
	// at least 1MB. The queue can support lower thresholds, but it will perform
	// terribly, so we give an explicit error in that case.
	// These bounds are still extremely low for Beats ingestion, but if all you
	// need is for a low-volume stream on a tiny device to persist between
	// restarts, it will work fine.
	if c.MaxSize != 0 && c.MaxSize < 10*1000*1000 {
		return fmt.Errorf(
			"disk queue max_size (%d) cannot be less than 10MB", c.MaxSize)
	}
	if c.SegmentSize != nil && *c.SegmentSize < 1000*1000 {
		return fmt.Errorf(
			"disk queue segment_size (%d) cannot be less than 1MB", *c.SegmentSize)
	}

	if c.RetryInterval != nil && c.MaxRetryInterval != nil &&
		*c.MaxRetryInterval < *c.RetryInterval {
		return fmt.Errorf(
			"disk queue max_retry_interval (%v) can't be less than retry_interval (%v)",
			*c.MaxRetryInterval, *c.RetryInterval)
	}

	return nil
}

func DiskSettingsFromConfig(diskConfig *DiskConfig) (diskqueue.Settings, error) {
	settings := defaultDiskqueueSettings()
	settings.Path = diskConfig.Path
	settings.MaxBufferSize = uint64(diskConfig.MaxSize)
	if diskConfig.SegmentSize != nil {
		settings.MaxSegmentSize = uint64(*diskConfig.SegmentSize)
	} else {
		// If no value is specified, default segment size is total queue size
		// divided by 10.
		settings.MaxSegmentSize = uint64(diskConfig.MaxSize) / 10
	}

	if diskConfig.ReadAheadLimit != nil {
		settings.ReadAheadLimit = *diskConfig.ReadAheadLimit
	}

	if diskConfig.WriteAheadLimit != nil {
		settings.WriteAheadLimit = *diskConfig.WriteAheadLimit
	}

	if diskConfig.RetryInterval != nil {
		settings.RetryInterval = *diskConfig.RetryInterval
	}
	if diskConfig.MaxRetryInterval != nil {
		settings.MaxRetryInterval = *diskConfig.RetryInterval
	}

	if diskConfig.EncryptionPassword != "" {
		// Use pbkdf2 to convert the string to a key of correct size
		settings.EncryptionKey = pbkdf2.Key([]byte(diskConfig.EncryptionPassword),
			[]byte(diskConfig.EncryptionPassword),
			4096,
			diskqueue.KeySize,
			sha1.New)
	}

	settings.UseCompression = diskConfig.UseCompression

	return settings, nil
}

func defaultDiskqueueSettings() diskqueue.Settings {
	settings := diskqueue.DefaultSettings()
	settings.UseProtobuf = true
	return settings
}
