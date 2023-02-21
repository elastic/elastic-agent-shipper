// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"crypto/tls"
	"net/http"
	"runtime"
	"time"

	"github.com/elastic/elastic-agent-libs/transport/httpcommon"
	"github.com/elastic/elastic-agent-libs/transport/tlscommon"
	"github.com/elastic/go-elasticsearch/v8"
)

// Config specifies all configurable parameters for the Elasticsearch output.
// Currently these are identical to the parameters in the Beats Elasticsearch
// output, however this is subject to change as we approach official release.
type Config struct {
	Enabled bool `config:"enabled"`

	// The following parameters map more-or-less directly to values in the
	// go-elasticsearch config, and are converted in the esConfig()
	// helper:

	Hosts    []string `config:"hosts"`
	Username string   `config:"username"`
	Password string   `config:"password"`

	// Default: {init: 1sec, max: 60sec}
	Backoff Backoff `config:"backoff"`

	// Default: 3
	MaxRetries int `config:"max_retries"`

	// Default: 502, 503, 504.
	RetryOnHTTPStatus []int `config:"retry_on_http_status"`

	Transport httpcommon.HTTPTransportSettings `config:",inline"`

	// The following parameters have no equivalent in the go-elasticsearch
	// config, and need to be specified directly when the indexer is created
	// instead of being returned in esConfig():

	// Defaults to runtime.NumCPU()
	NumWorkers int `config:"num_workers"`

	// Defaults to 5MB
	BatchSize int `config:"batch_size"`

	// Defaults to 30sec.
	FlushTimeout time.Duration `config:"flush_timeout"`

	// The following are internal parameters of the output that are not used
	// by go-elasticsearch:

	DegradedTimeout time.Duration `config:"degraded_timeout"`
}

// Backoff represents connection backoff settings
type Backoff struct {
	Init time.Duration `config:"init"`
	Max  time.Duration `config:"max"`
}

func (b Backoff) delayTime(attempt int) time.Duration {
	result := b.Init
	for i := 0; i < attempt; i++ {
		result *= 2
		if result >= b.Max {
			return b.Max
		}
	}
	return result
}

// Validate validates the ES config
func (c *Config) Validate() error {
	/*if c.APIKey != "" && (c.Username != "" || c.Password != "") {
		return fmt.Errorf("cannot set both api_key and username/password")
	}*/

	return nil
}

func (c Config) addDefaults() Config {
	if c.Backoff.Init == 0 {
		c.Backoff.Init = 1 * time.Second
	}
	if c.Backoff.Max == 0 {
		c.Backoff.Max = 60 * time.Second
	}
	if len(c.RetryOnHTTPStatus) == 0 {
		c.RetryOnHTTPStatus = []int{502, 503, 504}
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}
	if c.NumWorkers == 0 {
		c.NumWorkers = runtime.NumCPU()
	}
	if c.BatchSize == 0 {
		c.BatchSize = 5 * (1 << 20) // 5MB
	}
	if c.FlushTimeout == 0 {
		c.FlushTimeout = 30 * time.Second
	}
	return c
}

// esConfig converts the configuration for the elasticsearch shipper output
// to the configuration for the go-elasticsearch client API.
func (c Config) esConfig() elasticsearch.Config {
	c = c.addDefaults()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.Transport.TLS != nil && c.Transport.TLS.VerificationMode == tlscommon.VerifyNone {
		// Unlike Beats, the shipper doesn't support the ability to verify the
		// certificate but not the hostname, so any setting except VerifyNone
		// falls back on full verification.
		tlsConfig.InsecureSkipVerify = true
	}
	cfg := elasticsearch.Config{
		Addresses:     c.Hosts,
		Username:      c.Username,
		Password:      c.Password,
		RetryBackoff:  c.Backoff.delayTime,
		MaxRetries:    c.MaxRetries,
		RetryOnStatus: c.RetryOnHTTPStatus,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	return cfg
}
