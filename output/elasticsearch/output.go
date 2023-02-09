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

type ElasticSearchOutput struct {
	logger *logp.Logger
	config *Config

	queue *queue.Queue

	client        *elasticsearch.Client
	bulkIndexer   esutil.BulkIndexer
	healthWatcher ESHeathWatcher
	watchCancel   context.CancelFunc
	wg            sync.WaitGroup
}

// ESHeathWatcher monitors the ES connection and notifies if a failure persists for more than seconds
type ESHeathWatcher struct {
	reportFail      func(string)
	reportHealthy   func(string)
	lastSuccess     time.Time
	lastFailure     time.Time
	didFail         bool
	failureInterval time.Duration
	waitInterval    time.Duration

	mut sync.Mutex
}

func newHealthWatcher(reportFail, reportHealthy func(string), failureInterval time.Duration) ESHeathWatcher {
	return ESHeathWatcher{
		reportFail:      reportFail,
		reportHealthy:   reportHealthy,
		didFail:         false,
		failureInterval: failureInterval,
		waitInterval:    time.Second,
	}
}

func (hw *ESHeathWatcher) Fail() {
	hw.mut.Lock()
	defer hw.mut.Unlock()
	hw.lastFailure = time.Now()
}

func (hw *ESHeathWatcher) Success() {
	hw.mut.Lock()
	defer hw.mut.Unlock()
	hw.lastSuccess = time.Now()
}

func (hw *ESHeathWatcher) Watch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			break
		default:
		}
		hw.mut.Lock()
		// if the last success was > failure interval, and a failure was more recent, then report
		if time.Now().Sub(hw.lastSuccess) > hw.failureInterval && hw.lastFailure.After(hw.lastSuccess) && !hw.didFail {
			hw.didFail = true
			if hw.reportFail != nil {
				hw.reportFail("elasticsearch is degraded")
			}

		}
		if time.Now().Sub(hw.lastSuccess) < hw.failureInterval && hw.didFail {
			hw.didFail = false
			if hw.reportHealthy != nil {
				hw.reportHealthy("ES is sending events")
			}
		}
		hw.mut.Unlock()
		time.Sleep(hw.waitInterval)
	}
}

func NewElasticSearch(config *Config, reportDegradedCallback, reportRecoveredCallback func(string), queue *queue.Queue) *ElasticSearchOutput {
	out := &ElasticSearchOutput{
		logger:        logp.NewLogger("elasticsearch-output"),
		config:        config,
		queue:         queue,
		healthWatcher: newHealthWatcher(reportDegradedCallback, reportRecoveredCallback, config.DegradedTimeout),
	}

	return out
}

func serializeEvent(event *messages.Event) ([]byte, error) {
	// TODO: we need to preprocessing the raw protobuf to get fields in the
	// right place for ECS. This just translates the protobuf structure
	// directly to json.
	return json.Marshal(event)
}

func (es *ElasticSearchOutput) connWatch() {
	es.logger.Warnf("ES is unreachable for 30s")
}

func (es *ElasticSearchOutput) Start() error {
	client, err := elasticsearch.NewClient(es.config.esConfig())
	if err != nil {
		return err
	}
	healthTimeout := time.Second * 30
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Index:         "elastic-agent-shipper-test",
		Client:        client,
		NumWorkers:    1,
		FlushBytes:    1e+8, // 20MB
		FlushInterval: 30 * time.Second,
		Timeout:       healthTimeout,
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
			for _, event := range events {
				serialized, err := serializeEvent(event)
				if err != nil {
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
							es.healthWatcher.Fail()
							batch.Done(1)

						},
					},
				)
				if err != nil {
					es.logger.Errorf("couldn't add to bulk index request: %v", err)
					// This event couldn't be attempted, so mark it as finished.
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
