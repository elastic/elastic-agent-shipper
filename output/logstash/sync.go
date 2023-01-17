// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package logstash

import (
	"fmt"
	"time"

	"github.com/elastic/elastic-agent-libs/transport"
	"github.com/elastic/elastic-agent-libs/transport/tlscommon"
	v2 "github.com/elastic/go-lumber/client/v2"
)

type syncClient struct {
	conn   *transport.Client
	client *v2.SyncClient
	ttl    time.Duration
	ticker *time.Ticker
}

func newSyncClient(host string, config *Config) (*syncClient, error) {
	c := &syncClient{
		ttl: config.TTL,
	}
	tls, err := tlscommon.LoadTLSConfig(config.TLS)
	if err != nil {
		return nil, fmt.Errorf("error loading tls configuration: %w", err)
	}
	tp := transport.Config{
		Timeout: config.Timeout,
		Proxy:   &config.Proxy,
		TLS:     tls,
	}
	if c.ttl > 0 {
		c.ticker = time.NewTicker(c.ttl)
	}
	enc := makeLogstashEventEncoder()
	conn, err := transport.NewClient(tp, "tcp", host, defaultPort)
	if err != nil {
		return nil, fmt.Errorf("error creating connection to logstash host: %w", err)
	}
	c.conn = conn
	client, err := v2.NewSyncClientWithConn(conn,
		v2.JSONEncoder(enc),
		v2.Timeout(config.Timeout),
		v2.CompressionLevel(config.CompressionLevel),
	)
	if err != nil {
		return nil, fmt.Errorf("error creating logstash client, skipping: %w", err)
	}
	c.client = client
	return c, nil
}

func (c *syncClient) Reconnect() error {
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("error closing connection to logstash host %s: %w", c.conn.Host(), err)
	}
	return c.conn.Connect()
}
