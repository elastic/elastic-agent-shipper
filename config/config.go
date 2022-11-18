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

	"github.com/elastic/elastic-agent-shipper/monitoring"
	"github.com/elastic/elastic-agent-shipper/output"
	"github.com/elastic/elastic-agent-shipper/queue"
	"github.com/elastic/elastic-agent-shipper/server"

	"go.uber.org/zap/zapcore"
)

var (
	configFilePath string
	// ErrConfigIsNotSet reports that no unmanaged config file has been set
	ErrConfigIsNotSet = errors.New("config file is not set")
	fs                = flag.CommandLine
	// Overwrites is the config map of CLI overwrites set by the -E flag
	Overwrites = config.SettingFlag(fs, "E", "config overwrites")

	esKey      = "elasticsearch"
	consoleKey = "console"
)

// A lot of the code here is the same as what's in elastic-agent, but it lives in an internal/ library
func init() {
	fs.StringVar(&configFilePath, "c", "", "Run the shipper in the unmanaged mode and use the given configuration file instead")

}

// ShipperClientConfig is the shipper-relevant portion of the config received from input units
type ShipperClientConfig struct {
	Server string           `config:"server" mapstructure:"server"`
	TLS    ShipperClientTLS `config:"ssl" mapstructure:"ssl"`
}

// ShipperClientTLS is TLS-specific shipper client settings
type ShipperClientTLS struct {
	CAs  []string `config:"certificate_authorities" mapstructure:"certificate_authorities"`
	Cert string   `config:"certificate" mapstructure:"certificate"`
	Key  string   `config:"key" mapstructure:"key"`
}

// ShipperRootConfig defines the shipper config we get from elastic-agent's output unit
type ShipperRootConfig struct {
	Type    string        `config:"type"`
	Shipper ShipperConfig `config:"shipper"`
}

// ShipperConfig defines the config values stored under the `shipper` key in the fleet config
type ShipperConfig struct {
	// Don't know what logging config will look like via fleet yet,
	// and unpacking this is causing issues due to the manditory rotateeverybytes field
	//Log     logp.Config       `config:"logging"`
	Enabled bool              `config:"enabled"`
	Monitor monitoring.Config `config:"monitoring"` //Queue monitoring settings
	Queue   queue.Config      `config:"queue"`      //Queue settings
	Server  server.Config     `config:"server"`     //gRPC Server settings
	Output  output.Config     `config:"output"`     //Output settings
}

// DefaultConfig returns a default config for the shipper
func DefaultConfig() ShipperRootConfig {
	return ShipperRootConfig{
		Type: esKey,
		Shipper: ShipperConfig{
			Enabled: true,
			Monitor: monitoring.DefaultConfig(),
			Queue:   queue.DefaultConfig(),
			Server:  server.DefaultConfig(),
		},
	}
}

// ReadConfigFromFile returns the populated config from the specified path
func ReadConfigFromFile() (ShipperRootConfig, error) {
	if configFilePath == "" {
		return ShipperRootConfig{}, ErrConfigIsNotSet
	}
	contents, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return ShipperRootConfig{}, fmt.Errorf("error reading input file %s: %w", configFilePath, err)
	}

	raw, err := config.NewConfigWithYAML(contents, "")
	if err != nil {
		return ShipperRootConfig{}, fmt.Errorf("error reading config from yaml: %w", err)
	}

	unpacker := func(cfg *ShipperRootConfig) error {
		return raw.Unpack(cfg)
	}

	return readConfig(unpacker)
}

// ShipperConfigFromUnitConfig converts the configuration provided by Agent to the internal
// configuration object used by the shipper.
func ShipperConfigFromUnitConfig(level client.UnitLogLevel, rawConfig *proto.UnitExpectedConfig, unitID string) (ShipperRootConfig, error) {
	cfgObject := DefaultConfig()

	//TODO: Right now, don't update the log level, since I'm
	// not sure how to tell the elastic-agent to set it in debug
	logp.L().Debugf("Got new log level: %s", level.String())
	//logp.SetLevel(ZapFromUnitLogLevel(level))

	// Generate basic config object from the source
	// TODO: I would prefer to use the mapstructure library here,
	// since it's more ergonomic, but we import a bunch of libraries
	// into that config, all of which use our own `config` struct tag.
	mapCfg := rawConfig.GetSource().AsMap()
	cfg, err := config.NewConfigFrom(mapCfg)
	if err != nil {
		return ShipperRootConfig{}, fmt.Errorf("error reading in raw map config: %w", err)
	}

	//TODO: We should merge config overwrites here from the -E flag,
	// but they seem to step on the elasticsearch config, since there's no
	// so for now, don't.

	err = cfg.Unpack(&cfgObject)
	if err != nil {
		return ShipperRootConfig{}, fmt.Errorf("error unpacking shipper config: %w", err)
	}

	// TODO: hack, elastic-agent currently tries to start two shippers with the same config
	if unitID == "shipper-monitoring" {
		cfgObject.Shipper.Server.Port = 50052
	}

	// output config is at the "root" level, so we need to unpack those manually
	if cfgObject.Type == esKey {
		err = cfg.Unpack(&cfgObject.Shipper.Output.Elasticsearch)
		if err != nil {
			return ShipperRootConfig{}, fmt.Errorf("error reading elasticsearch output: %w", err)
		}
	} else if cfgObject.Type == consoleKey {
		err = cfg.Unpack(&cfgObject.Shipper.Output.Console)
		if err != nil {
			return ShipperRootConfig{}, fmt.Errorf("error reading console output: %w", err)
		}
	} else {
		return ShipperRootConfig{}, fmt.Errorf("error, could not find output for output key '%s'", cfgObject.Type)
	}
	return cfgObject, nil
}

type rawUnpacker func(cfg *ShipperRootConfig) error

func readConfig(unpacker rawUnpacker) (config ShipperRootConfig, err error) {
	// systemd environment will send us to stdout environment, which we want
	config = DefaultConfig()
	err = unpacker(&config)
	if err != nil {
		return config, fmt.Errorf("error unpacking shipper config: %w", err)
	}

	// TODO: uncommented for the standalone mode, since log setup is kind of in-progress
	// otherwise the logging configuration is just ignored
	// err = logp.Configure(config.Shipper.Log)
	// if err != nil {
	// 	return config, fmt.Errorf("error configuring the logger: %w", err)
	// }

	return config, nil
}

// ZapFromUnitLogLevel converts the log level used by the units API, to the one used by the logger
func ZapFromUnitLogLevel(level client.UnitLogLevel) zapcore.Level {
	newLevel := zapcore.InfoLevel
	switch level {
	case client.UnitLogLevelInfo:
		newLevel = zapcore.InfoLevel
	case client.UnitLogLevelDebug:
		newLevel = zapcore.DebugLevel
	case client.UnitLogLevelWarn:
		newLevel = zapcore.WarnLevel
	case client.UnitLogLevelError:
		newLevel = zapcore.ErrorLevel
	case client.UnitLogLevelTrace:
		// zapcore has no trace level
		newLevel = zapcore.DebugLevel
	}
	return newLevel
}
