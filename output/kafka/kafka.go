// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"github.com/Shopify/sarama"

	"github.com/elastic/beats/v7/libbeat/outputs/codec/json"

	"github.com/elastic/elastic-agent-libs/logp"
)
const (
	logSelector = "kafka"
)

func init() {
	sarama.Logger = kafkaLogger{log: logp.NewLogger(logSelector)}
}

// TODO: Fix up topic and codec creation
func makeKafka(
	config Config,
) (*Client, error) {

	log := logp.NewLogger(logSelector)

	log.Info("initialize kafka output")

	// TODO: Use the topic selector
	topic := config.Topic
	//topic, err := buildTopicSelector(&config)
	//if err != nil {
	//	return nil,nil
	//	//return outputs.Fail(err)
	//}

	libCfg, err := newSaramaConfig(log, config)
	if err != nil {
		log.Errorf("Sarama error %v", err)
		return nil, nil
	}

	hosts := config.Hosts

	// TODO: Fix encoding
	codec := json.New("1", json.Config{
		Pretty:     true,
		EscapeHTML: true,
	})

	//codec, err := codec.CreateEncoder(beat, config.Codec)
	//if err != nil {
	//	fmt.Println("failed %v", err)
	//	return nil, nil
	//	//return outputs.Fail(err)
	//}

	return newKafkaClient( /*observer, */ hosts, "kafka", config.Key, topic, config.Headers, codec, libCfg)

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
}

//// TODO: Topic interpolation...
//func buildTopicSelector(cfg *config.C) (outil.Selector, error) {
//	return outil.BuildSelectorFromConfig(cfg, outil.Settings{
//		Key:              "topic",
//		MultiKey:         "topics",
//		EnableSingleOnly: true,
//		FailEmpty:        true,
//		Case:             outil.SelectorKeepCase,
//	})
//	//return nil, nil
//}
