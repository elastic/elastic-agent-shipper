package output

import "github.com/elastic/elastic-agent-shipper/output/kafka"

type Config struct {
	output *kafka.Config `config:"kafka"`
}