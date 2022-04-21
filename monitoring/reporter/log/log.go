// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package log

import (
	"fmt"
	"time"

	"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
	"github.com/elastic/elastic-agent-libs/transform/typeconv"
	"github.com/elastic/elastic-agent-shipper/monitoring/reporter"
)

func init() {
	reporter.RegisterOutput("log", NewLoggerReporter)
}

// LoggerReporter handles the metrics reporter that writes directly to the configured logging output
// This uses the pre-existing global logger
type LoggerReporter struct {
	log *logp.Logger
}

// NewLoggerReporter returns a new reporter interface for the logger
func NewLoggerReporter(_ config.Namespace) (reporter.Reporter, error) {
	log := logp.L()
	log.Debugf("Starting metrics logging...")
	return LoggerReporter{log: logp.L()}, nil
}

// Update satisfies the reporter interface
func (rep LoggerReporter) Update(stats reporter.QueueMetrics) error {
	conv, err := toKeyValuePairs(stats)
	if err != nil {
		return fmt.Errorf("error updating metrics: %w", err)
	}
	rep.log.Infof("Raw formatted stats: %v", common.Time(time.Now()))
	rep.log.Infow("Queue Statistics", logp.Reflect("queue", conv))
	return nil
}

// Format the queue metrics in a way to make the Infow method happy
func toKeyValuePairs(info reporter.QueueMetrics) (mapstr.M, error) {
	args := mapstr.M{}

	err := typeconv.Convert(&args, info)
	if err != nil {
		return nil, fmt.Errorf("error converting queue metrics: %w", err)
	}

	return args, nil
}
