// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package config

import (
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/monitoring"
	"github.com/elastic/elastic-agent-shipper/queue"
	"github.com/elastic/elastic-agent-shipper/server"
	"github.com/elastic/go-ucfg/json"
)

const (
	defaultConfigName = "elastic-agent-shipper.yml"
	defaultPort       = 50051
)

var (
	configPath     string
	configFilePath string
)

// A lot of the code here is the same as what's in elastic-agent, but it lives in an internal/ library
func init() {
	fs := flag.CommandLine
	fs.StringVar(&configFilePath, "c", defaultConfigName, "Configuration file, relative to path.config")
	fs.StringVar(&configPath, "path.config", configPath, "Config path is the directory Agent looks for its config file")
}

//ShipperConfig defines the options present in the config file
type ShipperConfig struct {
	Log     logp.Config       `config:"logging"`
	TLS     bool              `config:"tls"`
	Cert    string            `config:"cert"`       //TLS cert file, if TLS is enabled
	Key     string            `config:"key"`        //TLS Keyfile, if specified
	Port    int               `config:"port"`       //Port to listen on
	Monitor monitoring.Config `config:"monitoring"` //Queue monitoring settings
	Queue   queue.Config      `config:"queue"`      //Queue settings
	Server  server.Config     `config:"server"`     //gRPC Server settings
}

// ReadConfig returns the populated config from the specified path
func ReadConfig() (ShipperConfig, error) {
	path := configFile()

	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return ShipperConfig{}, fmt.Errorf("error reading input file %s: %w", path, err)
	}

	raw, err := config.NewConfigWithYAML(contents, "")
	if err != nil {
		return ShipperConfig{}, fmt.Errorf("error reading config from yaml: %w", err)
	}
	// systemd environment will send us to stdout environment, which we want
	config := ShipperConfig{
		Port:    defaultPort,
		Log:     logp.DefaultConfig(logp.SystemdEnvironment),
		Monitor: monitoring.DefaultConfig(),
		Queue:   queue.DefaultConfig(),
		Server:  server.DefaultConfig(),
	}
	err = raw.Unpack(&config)
	if err != nil {
		return config, fmt.Errorf("error unpacking shipper config: %w", err)
	}
	return config, nil
}

// ReadConfigFromJSON reads the event in from a JSON config. I believe @blakerouse told me
// that the V2 controller will send events via JSON, but I could be wrong.
func ReadConfigFromJSON(raw string) (ShipperConfig, error) {
	rawCfg, err := json.NewConfig([]byte(raw))
	if err != nil {
		return ShipperConfig{}, fmt.Errorf("error parsing string config: %w", err)
	}
	shipperConfig := ShipperConfig{
		Port:    defaultPort,
		Log:     logp.DefaultConfig(logp.SystemdEnvironment),
		Monitor: monitoring.DefaultConfig(),
		Queue:   queue.DefaultConfig(),
		Server:  server.DefaultConfig(),
	}
	err = rawCfg.Unpack(&shipperConfig)
	if err != nil {
		return shipperConfig, fmt.Errorf("error unpacking shipper config: %w", err)
	}
	return shipperConfig, err
}

func configFile() string {
	if configFilePath == "" || configFilePath == defaultConfigName {
		return filepath.Join(configPath, defaultConfigName)
	}
	if filepath.IsAbs(configFilePath) {
		return configFilePath
	}
	return filepath.Join(configPath, configFilePath)
}
