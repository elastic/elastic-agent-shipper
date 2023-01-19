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
	"github.com/elastic/elastic-agent-libs/mapstr"

	"github.com/elastic/elastic-agent-shipper/monitoring"
	"github.com/elastic/elastic-agent-shipper/output"
	"github.com/elastic/elastic-agent-shipper/queue"

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
	kafaKey    = "kafka"
)

// A lot of the code here is the same as what's in elastic-agent, but it lives in an internal/ library
func init() {
	fs.StringVar(&configFilePath, "c", "", "Run the shipper in the unmanaged mode and use the given configuration file instead")

}

// ShipperClientConfig is the shipper-relevant portion of the config received from input units
type ShipperClientConfig struct {
	Server string     `config:"server" mapstructure:"server"`
	TLS    ShipperTLS `config:"ssl" mapstructure:"ssl"`
}

// ShipperTLS is TLS-specific shipper client settings
type ShipperTLS struct {
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
	Monitor monitoring.Config `config:"monitoring"` //Queue monitoring settings
	Queue   queue.Config      `config:"queue"`      //Queue settings
	Output  output.Config     `config:"output"`     //Output settings
	// StrictMode means that every incoming event will be validated against the
	// list of required fields. This introduces some additional overhead but can
	// be really handy for client developers on the debugging stage.
	// Normally, it should be disabled during production use and enabled for testing.
	// In production it is preferable to send events to the output if at all possible.
	StrictMode bool                `config:"strict_mode"`
	Server     ShipperClientConfig // server settings, set by the input unit
}

// DefaultConfig returns a default config for the shipper
func DefaultConfig() ShipperRootConfig {
	return ShipperRootConfig{
		Type: esKey,
		Shipper: ShipperConfig{
			Monitor: monitoring.DefaultConfig(),
			Queue:   queue.DefaultConfig(),
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
func ShipperConfigFromUnitConfig(level client.UnitLogLevel, rawConfig *proto.UnitExpectedConfig, grpcConfig ShipperClientConfig) (ShipperRootConfig, error) {
	cfgObject := DefaultConfig()

	logp.L().Debugf("Got new log level: %s", level.String())
	logp.SetLevel(ZapFromUnitLogLevel(level))

	// Generate basic config object from the source
	// I would prefer to use the mapstructure library here,
	// since it's more ergonomic, but we import a bunch of other structs
	// into this config, all of which use our own `config` struct tag.
	mapCfg := rawConfig.GetSource().AsMap()
	cfg, err := config.NewConfigFrom(mapCfg)
	if err != nil {
		return ShipperRootConfig{}, fmt.Errorf("error reading in raw map config: %w", err)
	}

	dbgMap := mapstr.M{}
	cfg.Unpack(&dbgMap)
	logp.L().Debugf("Got shipper output config: %s", dbgMap.StringToPrint())

	// We should merge config overwrites here from the -E flag,
	// but they seem to step on the elasticsearch config,
	// so for now, don't.

	err = cfg.Unpack(&cfgObject)
	if err != nil {
		return ShipperRootConfig{}, fmt.Errorf("error unpacking shipper config: %w", err)
	}

	cfgObject.Shipper.Server = grpcConfig

	// output config is at the "root" level, so we need to unpack those manually
	if cfgObject.Type == esKey {
		err = cfg.Unpack(&cfgObject.Shipper.Output.Elasticsearch)
		if err != nil {
			return ShipperRootConfig{}, fmt.Errorf("error reading elasticsearch output: %w", err)
		}
	} else if cfgObject.Type == kafaKey {
		err = cfg.Unpack(&cfgObject.Shipper.Output.Kafka)
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
