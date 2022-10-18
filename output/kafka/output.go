// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"context"
	//"fmt"
	"sync"

	//"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/beats/v7/libbeat/publisher"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/queue"
	//"github.com/elastic/beats/v7/libbeat/outputs/outil"
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
			for i := 0; i < batch.Count(); i++ {
				events[i] = batch.Entry(i).(*messages.Event)
			}

			for len(events) > 0 {
				events, _ = client.publishEvents(context.TODO(), events)
				// TODO: error handling / retry backoff?
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
	return nil
}

// Wait until the output loop has finished. This doesn't stop the
// loop by itself, so make sure you only call it when you close
// the queue.
func (out *KafkaOutput) Wait() {
	out.wg.Wait()
}

func makeKafka(
	config Config,
) (*Client, error) {
	log := logp.NewLogger(logSelector)
	log.Debug("initialize kafka output")

	//config, err := readConfig(config)
	//if err != nil {
	//	return outputs.Fail(err)
	//}

	//topic, err := buildTopicSelector(config)
	//if err != nil {
	//	return outputs.Fail(err)
	//}
	//
	//libCfg, err := newSaramaConfig(log, config)
	//if err != nil {
	//	return outputs.Fail(err)
	//}
	//
	//hosts, err := outputs.ReadHostList(config)
	//if err != nil {
	//	return outputs.Fail(err)
	//}
	//
	//codec, err := codec.CreateEncoder(beat, config.Codec)
	//if err != nil {
	//	return outputs.Fail(err)
	//}

	//client, err := newKafkaClient(observer, hosts, beat.IndexPrefix, config.Key, topic, config.Headers, codec, libCfg)
	//if err != nil {
	//	return outputs.Fail(err)
	//}
	//
	//retry := 0
	//if config.MaxRetries < 0 {
	//	retry = -1
	//}
	//return outputs.Success(config.BulkMaxSize, retry, client)
	return nil, nil
}

//func buildTopicSelector(cfg *Config) (outil.Selector, error) {
//	//return outil.BuildSelectorFromConfig(cfg, outil.Settings{
//	//	Key:              "topic",
//	//	MultiKey:         "topics",
//	//	EnableSingleOnly: true,
//	//	FailEmpty:        true,
//	//	Case:             outil.SelectorKeepCase,
//	//})
//	return nil, nil
//}

// Client provides the minimal interface an output must implement to be usable
// with the publisher pipeline.
type OldClientInterface interface {
	Close() error

	// Publish sends events to the clients sink. A client must synchronously or
	// asynchronously ACK the given batch, once all events have been processed.
	// Using Retry/Cancelled a client can return a batch of unprocessed events to
	// the publisher pipeline. The publisher pipeline (if configured by the output
	// factory) will take care of retrying/dropping events.
	// Context is intended for carrying request-scoped values, not for cancellation.
	Publish(context.Context, publisher.Batch) error

	// String identifies the client type and endpoint.
	String() string

	// Connect establishes a connection to the clients sink.
	// The connection attempt shall report an error if no connection could been
	// established within the given time interval. A timeout value of 0 == wait
	// forever.
	Connect() error
}

// NewClientInterface is a placeholder specifying the API I expect to implement while
// translating it from the Beats version
type NewClientInterface interface {
	Connect() error
	Close() error

	publishEvents(ctx context.Context, events []*messages.Event) ([]*messages.Event, error)
}
