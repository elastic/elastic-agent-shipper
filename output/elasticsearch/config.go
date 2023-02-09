// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/elastic/elastic-agent-libs/transport/httpcommon"
	"github.com/elastic/elastic-agent-libs/transport/tlscommon"
	"github.com/elastic/go-elasticsearch/v8"
)

// Config specifies all configurable parameters for the Elasticsearch output.
// Currently these are identical to the parameters in the Beats Elasticsearch
// output, however this is subject to change as we approach official release.
type Config struct {
	Enabled  bool     `config:"enabled"`
	Hosts    []string `config:"hosts"`
	Username string   `config:"username"`
	Password string   `config:"password"`

	Backoff    Backoff `config:"backoff"`
	MaxRetries int     `config:"max_retries"`

	Transport httpcommon.HTTPTransportSettings `config:",inline"`

	// The following configuration flags are copied from the equivalent Beats
	// configuration, but are commented out because they are not yet active/implemented:
	//Protocol           string            `config:"protocol"`
	//Path               string            `config:"path"`
	//Params             map[string]string `config:"parameters"`
	//Headers            map[string]string `config:"headers"`
	//APIKey             string            `config:"api_key"`
	//LoadBalance        bool              `config:"loadbalance"`
	//CompressionLevel   int               `config:"compression_level" validate:"min=0, max=9"`
	//EscapeHTML         bool              `config:"escape_html"`
	//Kerberos           *kerberos.Config  `config:"kerberos"`
	//NonIndexablePolicy *config.Namespace `config:"non_indexable_policy"`
	//AllowOlderVersion  bool              `config:"allow_older_versions"`

	// The following parameters have no equivalent in the go-elasticsearch
	// config, and need to be specified directly when the indexer is created
	// instead of being returned in esConfig().

	// Defaults to runtime.NumCPU()
	NumWorkers int `config:"num_workers"`

	// Defaults to 5MB
	BatchSize int `config:"batch_size"`

	// Defaults to 30sec.
	FlushTimeout time.Duration `config:"flush_timeout"`

	degradedTimeout time.Duration `config:"degraded_timeout"`
}

type Backoff struct {
	Init time.Duration
	Max  time.Duration
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

func (c *Config) Validate() error {
	/*if c.APIKey != "" && (c.Username != "" || c.Password != "") {
		return fmt.Errorf("cannot set both api_key and username/password")
	}*/

	return nil
}

// esConfig converts the configuration for the elasticsearch shipper output
// to the configuration for the go-elasticsearch client API.
func (c Config) esConfig() elasticsearch.Config {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.Transport.TLS != nil && c.Transport.TLS.VerificationMode == tlscommon.VerifyNone {
		// Unlike Beats, the shipper doesn't support the ability to verify the
		// certificate but not the hostname, so any setting except VerifyNone
		// falls back on full verification.
		tlsConfig.InsecureSkipVerify = true
	}
	cfg := elasticsearch.Config{
		Addresses:    c.Hosts,
		Username:     c.Username,
		Password:     c.Password,
		RetryBackoff: c.Backoff.delayTime,
		MaxRetries:   c.MaxRetries,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	return cfg
}
