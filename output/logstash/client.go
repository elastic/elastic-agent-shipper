// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package logstash

import (
	"fmt"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

const (
	defaultPort = 5044
)

type LogstashClient struct {
	config  *Config
	logger  *logp.Logger
	hosts   []*syncClient
	current int
}

func NewLogstashClient(config *Config, logger *logp.Logger) (*LogstashClient, error) {
	out := &LogstashClient{
		config: config,
		logger: logger,
	}
	for _, host := range config.Hosts {
		syncClient, err := newSyncClient(host, config)
		if err != nil {
			logger.Errorf("error creating client for logstash host %s, skipping: %w", host, err)
			continue
		}
		out.hosts = append(out.hosts, syncClient)
	}
	if len(out.hosts) == 0 {
		return nil, fmt.Errorf("no logstash hosts configured")
	}
	return out, nil
}

func (lc *LogstashClient) Publish(events []*messages.Event) (uint64, error) {
	ls_events := make([]interface{}, len(events))
	for i := range events {
		ls_events[i] = events[i]
	}
	for i, j := 0, lc.current; i < len(lc.hosts); i, j = i+1, (j+1)%len(lc.hosts) {
		lc.current = j
		sc := lc.hosts[j]
		if sc.ticker != nil {
			select {
			case <-sc.ticker.C:
				lc.logger.Debugf("ttl expired, reconnecting to logstash host: %s", sc.conn.Host())
				if err := sc.Reconnect(); err != nil {
					lc.logger.Errorf("error reconnecting to logstash host: %w", err)
					continue
				}
				// reset window size on reconnect
				//                                if c.win != nil {
				//      c.win.windowSize = int32(defaultStartMaxWindowSize)
				//                                }
			default:
			}
		}
		if !sc.conn.IsConnected() {
			lc.logger.Debugf("%s wasn't connected trying to connect", sc.conn.Host())
			if err := sc.conn.Connect(); err != nil {
				lc.logger.Errorf("error connecting to %s: %w", sc.conn.Host(), err)
				continue
			}
		}
		lc.logger.Debugf("sending %d events to %s", len(ls_events), sc.conn.Host())
		n, err := sc.client.Send(ls_events)
		if err != nil {
			lc.logger.Errorf("error sending to %s: %s", sc.conn.Host(), err)
			continue
		}
		return uint64(n), err
	}
	return 0, fmt.Errorf("couldn't publish to any host")
}
