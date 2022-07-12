// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"errors"
	"time"
)

type ShipperServerConfig struct {
	// PollingInterval is an interval for polling queue metrics and updating the indices.
	// Must be greater than 0
	PollingInterval time.Duration
}

// Validate validates the server configuration.
func (c ShipperServerConfig) Validate() error {
	if c.PollingInterval <= 0 {
		return errors.New("polling interval must be greater than 0")
	}

	return nil
}
