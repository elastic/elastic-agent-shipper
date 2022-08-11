// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

type Config struct {
	// StrictMode means that every incoming event will be validated against the
	// list of required fields. This introduces some additional overhead but can
	// be really handy for client developers on the debugging stage.
	// Normally, it should be disabled during production use and enabled for testing.
	StrictMode bool `config:"strict_mode"`
}

// DefaultConfig returns default configuration for the gRPC server
func DefaultConfig() Config {
	return Config{
		StrictMode: false,
	}
}
