// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"fmt"

	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

func getMetaStringValue(e *messages.Event, key string) (string, error) {
	meta := e.GetMetadata()
	metaMap := meta.GetData()
	value, ok := metaMap[key]
	if !ok {
		return "", fmt.Errorf("field not found")
	}
	return value.GetStringValue(), nil
}
