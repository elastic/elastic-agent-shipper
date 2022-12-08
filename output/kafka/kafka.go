// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"github.com/Shopify/sarama"

	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/outputs/codec"
	_ "github.com/elastic/beats/v7/libbeat/outputs/codec/format"
	_ "github.com/elastic/beats/v7/libbeat/outputs/codec/json"
	"github.com/elastic/beats/v7/libbeat/version"
	"github.com/elastic/elastic-agent-libs/logp"
)

const (
	logSelector = "kafka"
)

func init() {
	sarama.Logger = kafkaLogger{log: logp.NewLogger(logSelector)}
}

func makeKafka(
	config Config,
) (*Client, error) {

	log := logp.NewLogger(logSelector)

	log.Info("initialize kafka output")

	topic, err := buildTopicSelector(config)

	if err != nil {
		log.Errorf("Error building topic selector %v", err)
		return nil, nil
	}

	libCfg, err := newSaramaConfig(log, config)
	if err != nil {
		log.Errorf("Sarama error %v", err)
		return nil, nil
	}

	hosts := config.Hosts

	// The construction of the encoder requires a `beat.Info` object to be present
	// As far as I can tell, the only property used is the version, which is used
	// to populate metadata in the json encoder
	beatInfo := beat.Info{Version: version.GetDefaultVersion()}

	codec, err := codec.CreateEncoder(beatInfo, config.Codec)
	if err != nil {
		return nil, err
	}

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
