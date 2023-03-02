// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.elastic.co/fastjson"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/elastic/elastic-agent-shipper-client/pkg/helpers"
)

func TestEventMarshal(t *testing.T) {
	evt := Event{}
	ts := time.Now().UTC()
	fields, err := helpers.NewStruct(map[string]interface{}{
		"testval": "test",
		"data":    3,
		"map": map[string]interface{}{
			"nested": true,
		},
	})
	require.NoError(t, err)
	evt.Fields = fields
	evt.Timestamp = timestamppb.New(ts)

	expectedEvent := map[string]interface{}{
		"@timestamp": ts.Format(time.RFC3339Nano),
		"testval":    "test",
		"data":       float64(3),
		"map": map[string]interface{}{
			"nested": true,
		},
	}

	jsonWriter := &fastjson.Writer{}
	err = fastjson.Marshal(jsonWriter, &evt)
	require.NoError(t, err)

	// make sure it's valid JSON
	parsedEvt := map[string]interface{}{}
	err = json.Unmarshal(jsonWriter.Bytes(), &parsedEvt)
	require.NoError(t, err)
	if !reflect.DeepEqual(expectedEvent, parsedEvt) {
		t.Logf("expected equal events, got: ")
		require.Equal(t, expectedEvent, parsedEvt)
	}
}
