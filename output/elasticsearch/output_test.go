// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"encoding/json"
	"testing"

	"github.com/elastic/elastic-agent-libs/mapstr"
	"github.com/elastic/elastic-agent-shipper-client/pkg/helpers"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/stretchr/testify/require"
)

func TestOutputMarshal(t *testing.T) {
	// elastic-agent-client has a more comprehensive test of the custom marshal, this is just to make sure we didn't break something
	testMessage, err := helpers.NewStruct(map[string]interface{}{
		"testvalue": "value",
		"data":      3,
		"map": mapstr.M{
			"value": "string",
		},
	})
	require.NoError(t, err)

	evt := &messages.Event{Fields: testMessage}

	jsonData, err := serializeEvent(evt)
	require.NoError(t, err)
	// unmarshal back to map
	evtMap := mapstr.M{}
	t.Logf("got: %s", string(jsonData))
	err = json.Unmarshal(jsonData, &evtMap)
	require.NoError(t, err)
	require.Equal(t, "value", evtMap["testvalue"])
	require.Equal(t, float64(3), evtMap["data"])
	require.Equal(t, map[string]interface{}{"value": "string"}, evtMap["map"])
}
