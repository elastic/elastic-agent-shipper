// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package output

import (
	"github.com/elastic/elastic-agent-shipper/output/elasticsearch"
	"github.com/elastic/elastic-agent-shipper/output/kafka"
)


type ConsoleConfig struct {
	Enabled bool `config:"enabled"`
}

type Config struct {
	Console       *ConsoleConfig        `config:"console"`
	Elasticsearch *elasticsearch.Config `config:"elasticsearch"`
	Kafka         *kafka.Config         `config:"kafka"`
}

func DefaultConfig() Config {
	defaultKafka := kafka.DefaultConfig()
	return Config{
		Kafka: &defaultKafka,
	}
}