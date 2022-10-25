// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build !integration

package queue

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/config"
)

func TestDiskConfig(t *testing.T) {
	tests := map[string]struct {
		config         string
		errorCondition bool
		errorString    string
	}{
		"minimal": {
			config:         "path: \"/var/tmp\"\nmax_size: 10MB\n",
			errorCondition: false,
			errorString:    "",
		},
		"no max_size": {
			config:         "path: \"/var/tmp\"\n",
			errorCondition: true,
			errorString:    "missing required field accessing 'max_size'",
		},
		"negative retry_interval": {
			config:         "path: \"/var/tmp\"\nmax_size: 10MB\nretry_interval: -5m\n",
			errorCondition: true,
			errorString:    "negative value accessing 'retry_interval'",
		},
		"negative max_retry_interval": {
			config:         "path: \"/var/tmp\"\nmax_size: 10MB\nmax_retry_interval: -5m\n",
			errorCondition: true,
			errorString:    "negative value accessing 'max_retry_interval'",
		},
		"max_size < segment_size": {
			config:         "path: \"/var/tmp\"\nmax_size: 10MB\nsegment_size: 20MB\n",
			errorCondition: true,
			errorString:    "disk queue max_size must be at least twice as big as segment_size accessing config",
		},
		"max_size less than 10MB": {
			config:         "path: \"/var/tmp\"\nmax_size: 10KB\n",
			errorCondition: true,
			errorString:    "disk queue max_size (10000) cannot be less than 10MB accessing config",
		},
		"segment_size less than 1MB": {
			config:         "path: \"/var/tmp\"\nmax_size: 10MB\nsegment_size: 1KB\n",
			errorCondition: true,
			errorString:    "disk queue segment_size (1000) cannot be less than 1MB accessing config",
		},
		"max_retry_interval < retry_interval": {
			config:         "path: \"/var/tmp\"\nmax_size: 10MB\nmax_retry_interval: 1m\nretry_interval: 2m\n",
			errorCondition: true,
			errorString:    "disk queue max_retry_interval (1m0s) can't be less than retry_interval (2m0s) accessing config",
		},
	}

	for name, tc := range tests {
		cfg, err := config.NewConfigWithYAML([]byte(tc.config), "")
		require.NoErrorf(t, err, "%s: error making config from yaml: %s, %s", name, tc.config, err)
		config := DiskConfig{}
		err = cfg.Unpack(&config)
		if !tc.errorCondition && err != nil {
			t.Fatalf("%s: unexpected error: %s", name, err)
		}
		if tc.errorCondition && err == nil {
			t.Fatalf("%s: supposed to have error", name)
		}
		if tc.errorCondition {
			require.EqualErrorf(t, err, tc.errorString, "%s: errors don't match", name)
		}
	}
}
