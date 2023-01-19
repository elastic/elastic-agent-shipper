// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package logstash

import (
	"encoding/json"
)

// makeLogstashEventEncoder  TODO, evaluate if we need codec support
func makeLogstashEventEncoder() func(interface{}) ([]byte, error) {
	return func(event interface{}) (d []byte, err error) {
		return json.Marshal(event)
	}
}
