// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package logstash

import (
	"time"

	"github.com/elastic/elastic-agent-libs/config"

	"github.com/elastic/elastic-agent-libs/transport"
	"github.com/elastic/elastic-agent-libs/transport/tlscommon"
)

type Config struct {
	Enabled bool              `config:"enabled"`
	Hosts   []string          `config:"hosts"`
	TLS     *tlscommon.Config `config:"ssl"`
	//	LoadBalance bool              `config:"loadbalance"` TODO
	//      BulkMaxSize int `config:"bulk_max_size"` TODO
	//	SlowStart   bool          `config:"slow_start"` TODO
	Timeout time.Duration `config:"timeout"`
	TTL     time.Duration `config:"ttl"               validate:"min=0"`
	//	Pipelining       int                   `config:"pipelining"        validate:"min=0"` TODO support async client
	CompressionLevel int                   `config:"compression_level" validate:"min=0, max=9"`
	MaxRetries       int                   `config:"max_retries"       validate:"min=-1"`
	Proxy            transport.ProxyConfig `config:",inline"`
	//	Backoff          Backoff               `config:"backoff"`  TODO
}

func DefaultConfig() Config {
	return Config{
		CompressionLevel: 3,
		Timeout:          30 * time.Second,
		MaxRetries:       3,
		TTL:              0 * time.Second,
	}
}

func readConfig(cfg *config.C) (*Config, error) {
	c := DefaultConfig()

	if err := cfg.Unpack(&c); err != nil {
		return nil, err
	}

	return &c, nil
}
