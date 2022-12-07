// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/outputs/outil"
	libconfig "github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/mapstr"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

func mapstrForValue(v *messages.Value) interface{} {
	if boolVal, ok := v.GetKind().(*messages.Value_BoolValue); ok {
		return boolVal.BoolValue
	}
	if listVal, ok := v.GetKind().(*messages.Value_ListValue); ok {
		return mapstrForList(listVal.ListValue)
	}
	if nullVal, ok := v.GetKind().(*messages.Value_NullValue); ok {
		return nullVal.NullValue
	}
	if intVal, ok := v.GetKind().(*messages.Value_NumberValue); ok {
		return intVal.NumberValue
	}
	if strVal, ok := v.GetKind().(*messages.Value_StringValue); ok {
		return strVal.StringValue
	}
	if structVal, ok := v.GetKind().(*messages.Value_StructValue); ok {
		return mapstrForStruct(structVal.StructValue)
	}
	if tsVal, ok := v.GetKind().(*messages.Value_TimestampValue); ok {
		return tsVal.TimestampValue.AsTime()
	}
	return nil
}

func mapstrForList(list *messages.ListValue) []interface{} {
	results := []interface{}{}
	for _, val := range list.Values {
		results = append(results, mapstrForValue(val))
	}
	return results
}

func mapstrForStruct(proto *messages.Struct) mapstr.M {
	data := proto.GetData()
	result := mapstr.M{}
	for key, value := range data {
		result[key] = mapstrForValue(value)
	}
	return result
}

func beatsEventForProto(e *messages.Event) *beat.Event {
	return &beat.Event{
		Timestamp: e.GetTimestamp().AsTime(),
		Meta:      mapstrForStruct(e.GetMetadata()),
		Fields:    mapstrForStruct(e.GetFields()),
	}
}

func buildTopicSelectorFromConfig(config Config) (outil.Selector, error) {
	t := config.Topic
	ts := config.Topics
	configMap := map[string]interface{}{"topic": t, "topics": ts}
	cfg, err := libconfig.NewConfigFrom(configMap)
	if err != nil {
		return outil.Selector{}, err
	}
	return buildTopicSelector(cfg)
}

func buildTopicSelector(cfg *libconfig.C) (outil.Selector, error) {
	return outil.BuildSelectorFromConfig(cfg, outil.Settings{
		Key:              "topic",
		MultiKey:         "topics",
		EnableSingleOnly: true,
		FailEmpty:        true,
		Case:             outil.SelectorKeepCase,
	})
}
