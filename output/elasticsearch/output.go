// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/beats/v7/libbeat/esleg/eslegclient"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/queue"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
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

func serializeEvent(event *messages.Event) ([]byte, error) {
	return nil, nil
}

func (out *ElasticSearchOutput) Start() error {
	client, err := newMakeES()
	if err != nil {
		return err
	}
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Index:         "elastic-agent-shipper",
		Client:        client,
		NumWorkers:    1,
		FlushBytes:    int(0x10000000), // 256MB
		FlushInterval: 30 * time.Second,
	})
	if err != nil {
		return err
	}

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

			count := batch.Count()
			events := make([]*messages.Event, count)
			for i := 0; i < count; i++ {
				events[i], _ = batch.Entry(i).(*messages.Event)
			}
			completed := uint64(0)
			for _, event := range events {
				serialized, err := serializeEvent(event)
				if err != nil {
					out.logger.Errorf("failed to serialize event: %v", err)
					completed += 1
					continue
				}
				err = bi.Add(
					context.Background(),
					esutil.BulkIndexerItem{
						Action: "index",
						Body:   bytes.NewReader(serialized),
						OnSuccess: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem) {
							// TODO: update metrics
							if atomic.AddUint64(&completed, 1) >= uint64(count) {
								batch.Done()
							}
						},
						OnFailure: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem, err error) {
							// TODO: update metrics
							if atomic.AddUint64(&completed, 1) >= uint64(count) {
								batch.Done()
							}
						},
					},
				)
				if err != nil {
					out.logger.Errorf("couldn't add to bulk index request: %v", err)
				}
			}
		}
	}()
	return nil
}

// Wait until the output loop has finished. This doesn't stop the
// loop by itself, so make sure you only call it when you close
// the queue.
func (out *ElasticSearchOutput) Wait() {
	out.wg.Wait()
}

func newMakeES() (*elasticsearch.Client, error) {
	cfg := elasticsearch.Config{
		Addresses: []string{
			"https://localhost:9200",
		},
		Username: "elastic",
		Password: "pp3OwQRxejj_yt=fs*U-",
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	return elasticsearch.NewClient(cfg)
}

func oldMakeES(
	/*im outputs.IndexManager,
	beat beat.Info,
	observer outputs.Observer,*/
	config Config,
) (*Client, error) {
	log := logp.NewLogger("elasticsearch")
	/*if !cfg.HasField("bulk_max_size") {
		cfg.SetInt("bulk_max_size", -1, defaultBulkSize)
	}*/

	/*hosts, err := outputs.ReadHostList(cfg)
	if err != nil {
		return outputs.Fail(err)
	}*/

	if proxyURL := config.Transport.Proxy.URL; proxyURL != nil && !config.Transport.Proxy.Disable {
		log.Debugf("breaking down proxy URL. Scheme: '%s', host[:port]: '%s', path: '%s'", proxyURL.Scheme, proxyURL.Host, proxyURL.Path)
		log.Infof("Using proxy URL: %s", proxyURL)
	}

	params := config.Params
	if len(params) == 0 {
		params = nil
	}

	if len(config.Hosts) == 0 {
		return nil, fmt.Errorf("hosts list cannot be empty")
	}
	host := config.Hosts[0]
	esURL, err := common.MakeURL(config.Protocol, config.Path, host, 9200)
	if err != nil {
		log.Errorf("Invalid host param set: %s, Error: %+v", host, err)
		return nil, err
	}

	return NewClient(ClientSettings{
		ConnectionSettings: eslegclient.ConnectionSettings{
			URL:              esURL,
			Beatname:         "elastic-agent-shipper",
			Kerberos:         config.Kerberos,
			Username:         config.Username,
			Password:         config.Password,
			APIKey:           config.APIKey,
			Parameters:       params,
			Headers:          config.Headers,
			CompressionLevel: config.CompressionLevel,
			// TODO: No observer yet, is leaving it nil ok?
			EscapeHTML: config.EscapeHTML,
			Transport:  config.Transport,
		},
	}, &connectCallbackRegistry)
	/*clients := make([]*Client, len(hosts))
	for i, host := range hosts {
		esURL, err := common.MakeURL(config.Protocol, config.Path, host, 9200)
		if err != nil {
			log.Errorf("Invalid host param set: %s, Error: %+v", host, err)
			return nil, err
		}

		//var client outputs.NetworkClient
		client, err := NewClient(ClientSettings{
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
		}, &connectCallbackRegistry)
		if err != nil {
			return nil, err
		}

		clients[i] = client
	}

	return outputs.SuccessNet(config.LoadBalance, config.BulkMaxSize, config.MaxRetries, clients)*/
}
