// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/elastic/beats/v7/libbeat/common/transport/kerberos"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/transport/httpcommon"
	"github.com/elastic/elastic-agent-libs/transport/tlscommon"
	"github.com/elastic/go-elasticsearch/v8"
)

// Config specifies all configurable parameters for the Elasticsearch output.
// Currently these are identical to the parameters in the Beats Elasticsearch
// output, however this is subject to change as we approach official release.
type Config struct {
	Enabled            bool              `config:"enabled"`
	Hosts              []string          `config:"hosts"`
	Protocol           string            `config:"protocol"`
	Path               string            `config:"path"`
	Params             map[string]string `config:"parameters"`
	Headers            map[string]string `config:"headers"`
	Username           string            `config:"username"`
	Password           string            `config:"password"`
	APIKey             string            `config:"api_key"`
	LoadBalance        bool              `config:"loadbalance"`
	CompressionLevel   int               `config:"compression_level" validate:"min=0, max=9"`
	EscapeHTML         bool              `config:"escape_html"`
	Kerberos           *kerberos.Config  `config:"kerberos"`
	BulkMaxSize        int               `config:"bulk_max_size"`
	MaxRetries         int               `config:"max_retries"`
	Backoff            Backoff           `config:"backoff"`
	NonIndexablePolicy *config.Namespace `config:"non_indexable_policy"`
	AllowOlderVersion  bool              `config:"allow_older_versions"`

	Transport httpcommon.HTTPTransportSettings `config:",inline"`
}

type Backoff struct {
	Init time.Duration
	Max  time.Duration
}

/*const (
	defaultBulkSize = 50
)*/

/*var (
	defaultConfig = Config{
		Protocol:         "",
		Path:             "",
		Params:           nil,
		Username:         "",
		Password:         "",
		APIKey:           "",
		MaxRetries:       3,
		CompressionLevel: 0,
		EscapeHTML:       false,
		Kerberos:         nil,
		LoadBalance:      true,
		Backoff: Backoff{
			Init: 1 * time.Second,
			Max:  60 * time.Second,
		},
		Transport: httpcommon.DefaultHTTPTransportSettings(),
	}
)*/

func (c *Config) Validate() error {
	if c.APIKey != "" && (c.Username != "" || c.Password != "") {
		return fmt.Errorf("cannot set both api_key and username/password")
	}

	return nil
}

// esConfig converts the configuration for the elasticsearch shipper output
// to the configuration for the go-elasticsearch client API.
func (c *Config) esConfig() elasticsearch.Config {
	tlsConfig := &tls.Config{}
	if c.Transport.TLS.VerificationMode == tlscommon.VerifyNone {
		// Unlike Beats, the shipper doesn't support the ability to verify the
		// certificate but not the hostname, so any setting except VerifyNone
		// falls back on full verification.
		tlsConfig.InsecureSkipVerify = true
	}
	cfg := elasticsearch.Config{
		Addresses: c.Hosts,
		Username:  c.Username,
		Password:  c.Password,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	return cfg
}
