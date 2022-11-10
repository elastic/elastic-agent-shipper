// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package logstash

import (
	"testing"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/stretchr/testify/assert"
)

func TestNewLogstashClient(t *testing.T) {
	tests := map[string]struct {
		config    *Config
		errString string
	}{
		"one host": {
			config: &Config{
				Hosts: []string{"127.0.0.1:5044"},
			},
			errString: "",
		},
		"two host": {
			config: &Config{
				Hosts: []string{"127.0.0.1:5044", "127.0.0.2:5044"},
			},
			errString: "",
		},
		"no hosts": {
			config:    &Config{},
			errString: "no logstash hosts configured",
		},
	}
	logger := logp.NewLogger("logstash-output")
	for name, tc := range tests {
		client, err := NewLogstashClient(tc.config, logger)
		if tc.errString == "" {
			assert.NoErrorf(t, err, "%s: no error expected got: %s", name, err)
			assert.Equalf(t, len(client.hosts), len(tc.config.Hosts), "%s: expected %d hosts, got %d hosts", name, len(tc.config.Hosts), len(client.hosts))
		} else {
			assert.ErrorContainsf(t, err, tc.errString, "%s: expected error: %s, got error: %s", name, tc.errString, err)
		}

	}
}
