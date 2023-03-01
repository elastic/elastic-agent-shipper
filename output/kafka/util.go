// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/outputs/outil"
	libconfig "github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-shipper-client/pkg/helpers"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

func beatsEventForProto(e *messages.Event) *beat.Event {
	return &beat.Event{
		Timestamp: e.GetTimestamp().AsTime(),
		Meta:      helpers.AsMap(e.GetMetadata()),
		Fields:    helpers.AsMap(e.GetFields()),
	}
}

func buildTopicSelector(config Config) (outil.Selector, error) {
	t := config.Topic
	ts := config.Topics
	configMap := map[string]interface{}{"topic": t, "topics": ts}
	cfg, err := libconfig.NewConfigFrom(configMap)
	if err != nil {
		return outil.Selector{}, err
	}
	return outil.BuildSelectorFromConfig(cfg, outil.Settings{
		Key:              "topic",
		MultiKey:         "topics",
		EnableSingleOnly: true,
		FailEmpty:        true,
		Case:             outil.SelectorKeepCase,
	})
}
