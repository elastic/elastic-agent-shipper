// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package config

import (
	"flag"
	"fmt"

	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/monitoring"
)

const (
	defaultConfigName = "elastic-agent-shipper.yml"
	defaultPort       = 50051
)

var (
	configPath     string
	configFilePath string
	overwrites     = config.SettingFlag(nil, "E", "Configuration overwrite")
)

// A lot of the code here is the same as what's in elastic-agent, but it lives in an internal/ library
func init() {
	fs := flag.CommandLine
	fs.StringVar(&configFilePath, "c", defaultConfigName, "Configuration file, relative to path.config")
	fs.StringVar(&configPath, "path.config", configPath, "Config path is the directory Agent looks for its config file")
}

//ShipperConfig defines the options present in the config file
type ShipperConfig struct {
	TLS     bool              `config:"tls"`
	Cert    string            `config:"cert"`       //TLS cert file, if TLS is enabled
	Key     string            `config:"key"`        //TLS Keyfile, if specified
	Port    int               `config:"port"`       //Port to listen on
	Monitor monitoring.Config `config:"monitoring"` //Queue monitoring settings
}

func ReadConfigFromString(in string) (ShipperConfig, error) {
	raw, err := config.NewConfigWithYAML([]byte(in), "")
	if err != nil {
		return ShipperConfig{}, fmt.Errorf("error reading config from yaml: %w", err)
	}

	//merge the -E flags with the supplied config
	merged, err := config.MergeConfigs(raw, overwrites)
	if err != nil {
		return ShipperConfig{}, fmt.Errorf("error merging -E flags with existing config: %w", err)
	}

	baseConfig := ShipperConfig{
		Port:    defaultPort,
		Monitor: monitoring.DefaultConfig(),
	}

	// Doing this to wrap the current config used by the elastic-agent, not really sure what the final config will be for the shipper.
	wrapper := struct {
		Shipper ShipperConfig `struct:"shipper"`
	}{
		Shipper: baseConfig,
	}

	err = merged.Unpack(&wrapper)
	if err != nil {
		return baseConfig, fmt.Errorf("error unpacking shipper config: %w", err)
	}
	return wrapper.Shipper, nil
}

// GetLoggingConfig returns the logging config we get from the CLI
func GetLoggingConfig() (logp.Config, error) {
	wrapper := struct {
		Logging logp.Config `struct:"logging"`
	}{
		Logging: logp.DefaultConfig(logp.DefaultEnvironment),
	}

	// I'm sort of assuming that the agent will always configure logging over the CLI, since that's what it looks like from testing.
	err := overwrites.Unpack(&wrapper)
	if err != nil {
		return logp.Config{}, fmt.Errorf("error unpacking logging config: %w", err)
	}

	return wrapper.Logging, nil
}
