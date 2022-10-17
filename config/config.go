// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package config

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-client/v7/pkg/proto"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/go-ucfg/json"

	"github.com/elastic/elastic-agent-shipper/monitoring"
	"github.com/elastic/elastic-agent-shipper/output"
	"github.com/elastic/elastic-agent-shipper/queue"
	"github.com/elastic/elastic-agent-shipper/server"
)

var (
	configFilePath    string
	ErrConfigIsNotSet = errors.New("config file is not set")
)

// A lot of the code here is the same as what's in elastic-agent, but it lives in an internal/ library
func init() {
	fs := flag.CommandLine
	fs.StringVar(&configFilePath, "c", "", "Run the shipper in the unmanaged mode and use the given configuration file instead")
}

//ShipperConfig defines the options present in the config file
type ShipperConfig struct {
	Log     logp.Config       `config:"logging"`
	Monitor monitoring.Config `config:"monitoring"` //Queue monitoring settings
	Queue   queue.Config      `config:"queue"`      //Queue settings
	Server  server.Config     `config:"server"`     //gRPC Server settings
	Output  output.Config     `config:"output"`     //Output settings
}

// ReadConfigFromFile returns the populated config from the specified path
func ReadConfigFromFile() (ShipperConfig, error) {
	if configFilePath == "" {
		return ShipperConfig{}, ErrConfigIsNotSet
	}
	contents, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return ShipperConfig{}, fmt.Errorf("error reading input file %s: %w", configFilePath, err)
	}

	raw, err := config.NewConfigWithYAML(contents, "")
	if err != nil {
		return ShipperConfig{}, fmt.Errorf("error reading config from yaml: %w", err)
	}

	unpacker := func(cfg *ShipperConfig) error {
		return raw.Unpack(cfg)
	}

	return readConfig(unpacker)
}

// ShipperConfigFromUnitConfig converts the configuration provided by Agent to the internal
// configuration object used by the shipper.
// Currently this just converts the given struct to json and tries to deserialize it into the
// ShipperConfig struct. This is not the right way to do this, but this gets the build and
// tests passing again with the new version of elastic-agent-client. Migrating fully to this
// new config structure is part of the overall agent V2 transition, for more details see
// https://github.com/elastic/elastic-agent/issues/617.
func ShipperConfigFromUnitConfig(logLevel client.UnitLogLevel, config *proto.UnitExpectedConfig) (ShipperConfig, error) {
	jsonConfig, err := config.GetSource().MarshalJSON()
	if err != nil {
		return ShipperConfig{}, err
	}
	return ReadConfigFromJSON(string(jsonConfig))
}

// ReadConfigFromJSON reads the event in from a JSON config. I believe @blakerouse told me
// that the V2 controller will send events via JSON, but I could be wrong.
func ReadConfigFromJSON(raw string) (ShipperConfig, error) {
	rawCfg, err := json.NewConfig([]byte(raw))
	if err != nil {
		return ShipperConfig{}, fmt.Errorf("error parsing string config: %w", err)
	}

	unpacker := func(cfg *ShipperConfig) error {
		return rawCfg.Unpack(cfg)
	}

	return readConfig(unpacker)
}

type rawUnpacker func(cfg *ShipperConfig) error

func readConfig(unpacker rawUnpacker) (config ShipperConfig, err error) {
	// systemd environment will send us to stdout environment, which we want
	config = ShipperConfig{
		Log:     logp.DefaultConfig(logp.SystemdEnvironment),
		Monitor: monitoring.DefaultConfig(),
		Queue:   queue.DefaultConfig(),
		Server:  server.DefaultConfig(),
	}

	err = unpacker(&config)
	if err != nil {
		return config, fmt.Errorf("error unpacking shipper config: %w", err)
	}

	// otherwise the logging configuration is just ignored
	err = logp.Configure(config.Log)
	if err != nil {
		return config, fmt.Errorf("error configuring the logger: %w", err)
	}

	return config, nil
}
