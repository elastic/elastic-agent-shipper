// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/queue"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
)

// ElasticSearchOutput handles the Elasticsearch output connection
type ElasticSearchOutput struct {
	logger *logp.Logger
	config *Config

	queue *queue.Queue

	client        *elasticsearch.Client
	bulkIndexer   esutil.BulkIndexer
	healthWatcher ESHealthWatcher
	watchCancel   context.CancelFunc
	wg            sync.WaitGroup
}

// NewElasticSearch creates a new elasticsearch output
func NewElasticSearch(config *Config, reportCallback WatchReporter, queue *queue.Queue) *ElasticSearchOutput {
	if config.DegradedTimeout == 0 {
		config.DegradedTimeout = time.Second * 30
	}
	out := &ElasticSearchOutput{
		logger:        logp.NewLogger("elasticsearch-output"),
		config:        config,
		queue:         queue,
		healthWatcher: newHealthWatcher(reportCallback, config.DegradedTimeout),
	}

	return out
}

func serializeEvent(event *messages.Event) ([]byte, error) {
	// TODO: we need to preprocessing the raw protobuf to get fields in the
	// right place for ECS. This just translates the protobuf structure
	// directly to json.
	return json.Marshal(event)
}

// Start the elasticsearch output
func (es *ElasticSearchOutput) Start() error {
	es.logger.Debugf("Starting elasticsearch output")
	client, err := elasticsearch.NewClient(es.config.esConfig())
	if err != nil {
		return err
	}
	healthTimeout := time.Second * 30
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Index:         "elastic-agent-shipper-test",
		Client:        client,
		Timeout:       healthTimeout,
		NumWorkers:    es.config.NumWorkers,
		FlushBytes:    es.config.BatchSize,
		FlushInterval: es.config.FlushTimeout,
	})
	if err != nil {
		return err
	}
	es.client = client
	es.bulkIndexer = bi

	ctx, cancel := context.WithCancel(context.Background())
	es.watchCancel = cancel
	go es.healthWatcher.Watch(ctx)

	es.wg.Add(1)
	go func() {
		defer es.wg.Done()
		for {
			batch, err := es.queue.Get(1000)
			// Once an output receives a batch, it is responsible for
			// it until all events have been either successfully sent or
			// discarded after failure.
			if err != nil {
				// queue.Get can only fail if the queue was closed,
				// time for the output to shut down.
				break
			}

			// Add this batch to the shutdown wait group and release it
			// in the batch's completion callback
			es.wg.Add(1)
			batch.CompletionCallback = es.wg.Done

			events := batch.Events()
			es.logger.Debugf("Output got batch of %d events", len(events))
			for _, event := range events {
				serialized, err := serializeEvent(event)
				if err != nil {
					es.healthWatcher.Fail(err.Error())
					es.logger.Errorf("failed to serialize event: %v", err)
					continue
				}
				err = bi.Add(
					context.Background(),
					esutil.BulkIndexerItem{
						Action: "index",
						Body:   bytes.NewReader(serialized),
						OnSuccess: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem) {
							// TODO: update metrics
							es.healthWatcher.Success()
							batch.Done(1)
						},
						OnFailure: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem, err error) {
							// TODO: update metrics
							es.healthWatcher.Fail(res.Error.Cause.Reason)
							es.logger.Debugf("Failed to add items: %#v", res.Error.Cause.Reason)
							batch.Done(1)

						},
					},
				)
				if err != nil {
					es.healthWatcher.Fail(err.Error())
					es.logger.Errorf("couldn't add to bulk index request: %v", err)
					// This event couldn't be attempted, so mark it as finished.
					batch.Done(1)
				}
			}
		}

		// Close the bulk indexer
		if err := bi.Close(context.Background()); err != nil {
			es.logger.Errorf("error closing bulk indexer: %s", err)
		}
	}()
	return nil
}

// Wait until the output loop has finished and pending events have concluded in
// success or failure. This doesn't stop the output loop by itself, so make sure
// you only call it when the queue is closed.
func (es *ElasticSearchOutput) Wait() {
	if es.watchCancel != nil {
		es.watchCancel()
	}
	es.wg.Wait()
}
