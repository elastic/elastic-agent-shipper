// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
    "github.com/elastic/beats/v7/libbeat/beat"
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

//// BuildSelectorFromConfig creates a selector from a configuration object.
//func BuildSelectorFromConfig(
//    cfg *Config,
//    settings Settings,
//) (Selector, error) {
//    var sel []SelectorExpr
//
//    key := settings.Key
//    multiKey := settings.MultiKey
//    found := false
//
//    if cfg.HasField(multiKey) {
//        found = true
//        sub, err := cfg.Child(multiKey, -1)
//        if err != nil {
//            return Selector{}, err
//        }
//
//        var table []*config.C
//        if err := sub.Unpack(&table); err != nil {
//            return Selector{}, err
//        }
//
//        for _, config := range table {
//            action, err := buildSingle(config, key, settings.Case)
//            if err != nil {
//                return Selector{}, err
//            }
//
//            if action != nilSelector {
//                sel = append(sel, action)
//            }
//        }
//    }
//
//    if settings.EnableSingleOnly && cfg.HasField(key) {
//        found = true
//
//        // expect event-format-string
//        str, err := cfg.String(key, -1)
//        if err != nil {
//            return Selector{}, err
//        }
//
//        fmtstr, err := fmtstr.CompileEvent(str)
//        if err != nil {
//            return Selector{}, fmt.Errorf("%v in %v", err, cfg.PathOf(key))
//        }
//
//        fmtsel, err := FmtSelectorExpr(fmtstr, "", settings.Case)
//        if err != nil {
//            return Selector{}, fmt.Errorf("%v in %v", err, cfg.PathOf(key))
//        }
//
//        if fmtsel != nilSelector {
//            sel = append(sel, fmtsel)
//        }
//    }
//
//    if settings.FailEmpty && !found {
//        if settings.EnableSingleOnly {
//            return Selector{}, fmt.Errorf("missing required '%v' or '%v' in %v",
//                key, multiKey, cfg.Path())
//        }
//
//        return Selector{}, fmt.Errorf("missing required '%v' in %v",
//            multiKey, cfg.Path())
//    }
//
//    return MakeSelector(sel...), nil
//}
