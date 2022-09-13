package elasticsearch

import (
	"fmt"
	"sync"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/queue"
)

type ElasticSearchOutput struct {
	logger *logp.Logger
	config *Config

	queue *queue.Queue

	wg sync.WaitGroup
}

func NewElasticSearch(config *Config, queue *queue.Queue) *ElasticSearchOutput {
	out := &ElasticSearchOutput{
		logger: logp.NewLogger("elasticsearch-output"),
		config: config,
		queue:  queue,
	}

	return out
}

func (out *ElasticSearchOutput) Start() {
	out.wg.Add(1)
	go func() {
		defer out.wg.Done()
		for {
			batch, err := out.queue.Get(1000)
			// Once an output receives a batch, it is responsible for
			// it until all events have been either successfully sent or
			// discarded after failure.
			if err != nil {
				// queue.Get can only fail if the queue was closed,
				// time for the output to shut down.
				break
			}
			for i := 0; i < batch.Count(); i++ {
				if event, ok := batch.Entry(i).(*messages.Event); ok {
					out.send(event)
				}
			}
			// This tells the queue that we're done with these events
			// and they can be safely discarded. The Beats queue interface
			// doesn't provide a way to indicate failure, of either the
			// full batch or individual events. The plan is for the
			// shipper to track events by their queue IDs so outputs
			// can report status back to the server; see
			// https://github.com/elastic/elastic-agent-shipper/issues/27.
			batch.Done()
		}
	}()
}

func (*ElasticSearchOutput) send(event *messages.Event) {
	//nolint: forbidigo // Console output is intentional
	fmt.Printf("%v\n", event)
}

// Wait until the output loop has finished. This doesn't stop the
// loop by itself, so make sure you only call it when you close
// the queue.
func (out *ElasticSearchOutput) Wait() {
	out.wg.Wait()
}

/*func makeES(
	im outputs.IndexManager,
	beat beat.Info,
	observer outputs.Observer,
	cfg *config.C,
) (outputs.Group, error) {
	log := logp.NewLogger(logSelector)
	if !cfg.HasField("bulk_max_size") {
		cfg.SetInt("bulk_max_size", -1, defaultBulkSize)
	}

	index, pipeline, err := buildSelectors(im, beat, cfg)
	if err != nil {
		return outputs.Fail(err)
	}

	config := defaultConfig
	if err := cfg.Unpack(&config); err != nil {
		return outputs.Fail(err)
	}

	policy, err := newNonIndexablePolicy(config.NonIndexablePolicy)
	if err != nil {
		log.Errorf("error while creating file identifier: %v", err)
		return outputs.Fail(err)
	}

	hosts, err := outputs.ReadHostList(cfg)
	if err != nil {
		return outputs.Fail(err)
	}

	if proxyURL := config.Transport.Proxy.URL; proxyURL != nil && !config.Transport.Proxy.Disable {
		log.Debugf("breaking down proxy URL. Scheme: '%s', host[:port]: '%s', path: '%s'", proxyURL.Scheme, proxyURL.Host, proxyURL.Path)
		log.Infof("Using proxy URL: %s", proxyURL)
	}

	params := config.Params
	if len(params) == 0 {
		params = nil
	}

	if policy.action() == dead_letter_index {
		index = DeadLetterSelector{
			Selector:        index,
			DeadLetterIndex: policy.index(),
		}
	}

	clients := make([]outputs.NetworkClient, len(hosts))
	for i, host := range hosts {
		esURL, err := common.MakeURL(config.Protocol, config.Path, host, 9200)
		if err != nil {
			log.Errorf("Invalid host param set: %s, Error: %+v", host, err)
			return outputs.Fail(err)
		}

		var client outputs.NetworkClient
		client, err = NewClient(ClientSettings{
			ConnectionSettings: eslegclient.ConnectionSettings{
				URL:              esURL,
				Beatname:         beat.Beat,
				Kerberos:         config.Kerberos,
				Username:         config.Username,
				Password:         config.Password,
				APIKey:           config.APIKey,
				Parameters:       params,
				Headers:          config.Headers,
				CompressionLevel: config.CompressionLevel,
				Observer:         observer,
				EscapeHTML:       config.EscapeHTML,
				Transport:        config.Transport,
			},
			Index:              index,
			Pipeline:           pipeline,
			Observer:           observer,
			NonIndexableAction: policy.action(),
		}, &connectCallbackRegistry)
		if err != nil {
			return outputs.Fail(err)
		}

		client = outputs.WithBackoff(client, config.Backoff.Init, config.Backoff.Max)
		clients[i] = client
	}

	return outputs.SuccessNet(config.LoadBalance, config.BulkMaxSize, config.MaxRetries, clients)
}*/
