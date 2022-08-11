package output

import "github.com/elastic/elastic-agent-shipper/output/elasticsearch"

type Config struct {
	elasticsearch *elasticsearch.Config `config:"elasticsearch"`
}
