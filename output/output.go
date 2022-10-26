package output

import (
	"github.com/elastic/elastic-agent-shipper/output/kafka"
)


type ConsoleConfig struct {
	Enabled bool `config:"enabled"`
}

type Config struct {
	Console       *ConsoleConfig        `config:"console"`
	Kafka         *kafka.Config         `config:"kafka"`
}

func DefaultConfig() Config {
	defaultKafka := kafka.DefaultConfig()
	return Config{
		Kafka: &defaultKafka,
	}
}