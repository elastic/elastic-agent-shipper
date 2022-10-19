package output

import "github.com/elastic/elastic-agent-shipper/output/elasticsearch"

type ConsoleConfig struct {
	Enabled bool `config:"enabled"`
}

type Config struct {
	Console       *ConsoleConfig        `config:"console"`
	Elasticsearch *elasticsearch.Config `config:"elasticsearch"`
}
