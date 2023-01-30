// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/elastic-agent-libs/config"
)

func TestValidConfig(t *testing.T) {
	cfg := config.MustNewConfigFrom("enabled: true")
	result, err := readConfig(cfg)
	if err != nil {
		t.Fatalf("Can't create test configuration from valid input")
	}
	assert.Equal(t, *result, Config{Enabled: true})
}

func TestInvalidConfig(t *testing.T) {
	// TODO: Add a real test case here when we do enough config validation
	// that an invalid outcome is possible.
}

func readConfig(cfg *config.C) (*Config, error) {
	c := Config{}
	if err := cfg.Unpack(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
