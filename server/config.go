// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

const (
	defaultPort = 50051
)

type Config struct {
	// StrictMode means that every incoming event will be validated against the
	// list of required fields. This introduces some additional overhead but can
	// be really handy for client developers on the debugging stage.
	// Normally, it should be disabled during production use and enabled for testing.
	// In production it is preferable to send events to the output if at all possible.
	StrictMode bool `config:"strict_mode"`
	// Whether to use TLS for the gRPC connection.
	TLS bool `config:"tls"`
	// TLS cert file, if TLS is enabled
	Cert string `config:"cert"`
	// TLS Keyfile, if specified
	Key string `config:"key"`
	// Port to listen on
	Port int `config:"port"`
}

// DefaultConfig returns default configuration for the gRPC server
func DefaultConfig() Config {
	return Config{
		StrictMode: false,
		TLS:        false,
		Port:       defaultPort,
	}
}
