// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"sync"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/queue"
)

type KafkaOutput struct {
	logger *logp.Logger
	config *Config

	queue *queue.Queue

	wg sync.WaitGroup
}

func NewKafka(config *Config, queue *queue.Queue) *KafkaOutput {
	out := &KafkaOutput{
		logger: logp.NewLogger("kafka-output"),
		config: config,
		queue:  queue,
	}

	return out
}

func (out *KafkaOutput) Start() error {
	client, err := makeKafka(*out.config)
	if err != nil {
		client.log.Errorf("Unable to make kafka output %v", err)
		return err
	}

	err = client.Connect()
	if err != nil {
		client.log.Errorf("Unable to connect to client %v", err)
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
				client.log.Errorf("Shutting output down, queue closed: %v", err)
				break
			}
			events := batch.Events()
			remaining := len(events)
			for len(events) > 0 {
				client.log.Debugf("Sending %d events to client to publish", len(events))
				events, _ = client.publishEvents(events)
				completed := remaining - len(events)
				remaining = len(events)
				batch.Done(completed)
				// TODO: error handling / retry backoff?
				client.log.Debugf("Finished sending batch with %v errors", len(events))
			}
			// This tells the queue that we're done with these events
			// and they can be safely discarded. The Beats queue interface
			// doesn't provide a way to indicate failure, of either the
			// full batch or individual events. The plan is for the
			// shipper to track events by their queue IDs so outputs
			// can report status back to the server; see
			// https://github.com/elastic/elastic-agent-shipper/issues/27.
			batch.Done(remaining)
		}
	}()
	return nil
}

// Wait until the output loop has finished. This doesn't stop the
// loop by itself, so make sure you only call it when you close
// the queue.
func (out *KafkaOutput) Wait() {
	out.wg.Wait()
}
