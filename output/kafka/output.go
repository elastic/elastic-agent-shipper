// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"context"
	"fmt"
	"sync"

	//"github.com/elastic/beats/v7/libbeat/common"
	//"github.com/elastic/beats/v7/libbeat/publisher"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/queue"
	//"github.com/elastic/beats/v7/libbeat/outputs/outil"
)

type Output struct {
	logger *logp.Logger
	config Config

	queue *queue.Queue

	wg sync.WaitGroup
}


func NewKafka(config Config, queue *queue.Queue) *Output {
	out := &Output{
		logger: logp.NewLogger("kafka-output"),
		config: config,
		queue:  queue,
	}

	return out
}


func (out *Output) Start() error {
	fmt.Printf("Making kafka\n")
	client, err := makeKafka(out.config)
	if err != nil {
		fmt.Printf("Making error with %s", err)
		return err
	}
	fmt.Printf("Made kafka client with %s", client)
    client.Connect()

	out.wg.Add(1)
	go func() {
		defer out.wg.Done()
		for {

			batch, err := out.queue.Get(1000)
			fmt.Printf("Got a batch of %s", batch)
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
				fmt.Println("Trying to publish %s events", len(events))
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
func (out *Output) Wait() {
	out.wg.Wait()
}

func makeKafka(
	config Config,
) (*Client, error) {
	fmt.Printf("config read %s\n", config)

	log := logp.NewLogger("kafka-output")
	fmt.Printf("creating log %s", log)

	log.Info("initialize kafka output")

	//topic, err := buildTopicSelector(config)
	//if err != nil {
	//	return outputs.Fail(err)
	//}
	//

	libCfg, err := newSaramaConfig(log, config)
	if err != nil {
		fmt.Println("Sarama error %s\n", err)
		return nil, nil
	}

	hosts := config.Hosts

	// TODO: Figure out what to do with codec
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

	topic := config.Topic

	client, err := newKafkaClient(/*observer, */hosts, /*beat.IndexPrefix, */config.Key, topic, config.Headers, /*codec, */libCfg)

	// TODO: Make sure this is what we want to do with our return values, or whether we need to utilize the Success object
	//if err != nil {
	//	return outputs.Fail(err)
	//}
	//
	//retry := 0
	//if config.MaxRetries < 0 {
	//	retry = -1
	//}
	//return outputs.Success(config.BulkMaxSize, retry, client)
	return client, nil
}

// TODO: Topic interpolation...
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
