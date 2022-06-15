// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigIngest(t *testing.T) {
	in := `
shipper:
    port: 50052
    tls: false
    #cert: # path to TLS cert
    #key: # path to TLS keyfile

    #log level
    logging.level: debug
    logging.selectors: ["*"]
    logging.to_stderr: true

    queue:
      test: #There is no actual "test" queue type, remove this later.
        events: 512

    monitoring:
      enabled: true
      interval: 5s
      log: true
      http:
        enabled: true
        host: "not_localhost"
        port: 8484
        name: "queue"

    outputs:
      default:
        type: elasticsearch
        hosts: [127.0.0.1:9200]
        api-key: "example-key"
        # username: "elastic"
        # password: "changeme"
`

	out, err := ReadConfigFromString(in)
	require.NoError(t, err)
	t.Logf("Got config: %#v", out)
	assert.Equal(t, out.Port, 50052)
	assert.Equal(t, out.Monitor.ExpvarOutput.Host, "not_localhost")
	assert.Equal(t, out.Monitor.ExpvarOutput.Port, 8484)
}
