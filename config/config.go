// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package config

import (
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
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
	Log  logp.Config `config:"logging"`
	TLS  bool        `config:"tls"`
	Cert string      `config:"cert"` //TLS cert file, if TLS is enabled
	Key  string      `config:"key"`  //TLS Keyfile, if specified
	Port int         `config:"port"` //Port to listen on
}

// ReadConfig returns the populated config from the specified path
func ReadConfig() (ShipperConfig, error) {
	path := configFile()

	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return ShipperConfig{}, fmt.Errorf("Error reading input file %s: %v", path, err)
	}

	raw, err := config.NewConfigWithYAML(contents, "")
	// TODO: This logging init will need to be a tad more sophisticated, I assume there's something in the libs already
	config := ShipperConfig{Port: defaultPort, Log: logp.DefaultConfig(logp.DefaultEnvironment)}
	err = raw.Unpack(&config)
	if err != nil {
		return config, fmt.Errorf("error unpacking config: %w", err)
	}

	return config, nil
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
