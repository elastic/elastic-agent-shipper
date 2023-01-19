// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package logstash

import (
	"fmt"
	"sync"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/queue"
)

type LogstashOutput struct {
	logger *logp.Logger
	config *Config
	queue  *queue.Queue
	wg     sync.WaitGroup
	client *LogstashClient
}

func NewLogstash(config *Config, queue *queue.Queue) *LogstashOutput {
	out := &LogstashOutput{
		logger: logp.NewLogger("logstash-output"),
		config: config,
		queue:  queue,
	}
	return out
}

func (ls *LogstashOutput) Start() error {
	client, err := NewLogstashClient(ls.config, ls.logger)
	if err != nil {
		return fmt.Errorf("error creating logstash client: %w", err)
	}
	ls.client = client
	ls.wg.Add(1)
	go func() {
		defer ls.wg.Done()
		for {
			batch, err := ls.queue.Get(1000)
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
			ls.wg.Add(1)
			batch.CompletionCallback = ls.wg.Done

			events := batch.Events()
			remaining := uint64(len(events))
			completed, err := ls.client.Publish(events)
			if err != nil {
				ls.logger.Errorf("failed to send %d out of %d events: %w", remaining-completed, remaining, err)
				batch.Done(remaining)
				continue
			}
			if completed != remaining {
				ls.logger.Errorf("Didn't send all events but no error")
				batch.Done(remaining)
				continue
			}
			batch.Done(remaining)
		}
	}()
	return nil
}

func (ls *LogstashOutput) Wait() {
	ls.wg.Wait()
}
