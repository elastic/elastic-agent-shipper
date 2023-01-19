// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package logstash

import (
	"testing"
	"time"

	"github.com/elastic/elastic-agent-libs/config"

	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {
	for name, test := range map[string]struct {
		config         *config.C
		expectedConfig *Config
		err            bool
	}{
		"default config": {
			config: config.MustNewConfigFrom([]byte(`{ }`)),
			expectedConfig: &Config{
				CompressionLevel: 3,
				Timeout:          30 * time.Second,
				MaxRetries:       3,
				TTL:              0 * time.Second,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := readConfig(test.config)
			if test.err {
				assert.Error(t, err)
				assert.Nil(t, cfg)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, test.expectedConfig, cfg)
			}
		})
	}
}
