// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

/*
import (
	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/outputs"
)

type DeadLetterSelector struct {
	Selector        outputs.IndexSelector
	DeadLetterIndex string
}

func (d DeadLetterSelector) Select(event *beat.Event) (string, error) {
	result, _ := event.Meta.HasKey(dead_letter_marker_field)
	if result {
		return d.DeadLetterIndex, nil
	}
	return d.Selector.Select(event)
}
*/
