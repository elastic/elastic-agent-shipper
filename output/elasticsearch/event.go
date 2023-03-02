// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"fmt"
	"time"

	"go.elastic.co/fastjson"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

// Event is a wrapper type for beat events
// the events that ES expects contain an @timestamp at the root evel
type Event struct {
	Timestamp *timestamppb.Timestamp
	Fields    *messages.Struct
}

// MarshalFastJSON implements the JSON interface for the main event type
func (evt *Event) MarshalFastJSON(w *fastjson.Writer) error {
	w.RawByte('{')
	w.RawString("\"@timestamp\":")
	w.RawByte('"')
	w.Time(evt.Timestamp.AsTime(), time.RFC3339Nano)
	w.RawByte('"')

	if evt.Fields != nil {
		w.RawByte(',')
		beginning := true
		for key, val := range evt.Fields.GetData() {
			if !beginning {
				w.RawByte(',')
			} else {
				beginning = false
			}

			w.RawString(fmt.Sprintf("\"%s\":", key))
			err := val.MarshalFastJSON(w)
			if err != nil {
				return fmt.Errorf("error marshaling value in map: %w", err)
			}
		}
	}

	w.RawByte('}')
	return nil
}
