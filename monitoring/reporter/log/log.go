// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package log

import (
	"fmt"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
	"github.com/elastic/elastic-agent-libs/transform/typeconv"
	"github.com/elastic/elastic-agent-shipper/monitoring/reporter"
)

// LoggerReporter handles the metrics reporter that writes directly to the configured logging output
// This uses the pre-existing global logger
type LoggerReporter struct {
	log *logp.Logger
}

// Config is the config object for the log output
type Config struct {
	Enabled bool `config:"enabled"`
}

// NewLoggerReporter returns a new reporter interface for the logger
func NewLoggerReporter() reporter.Reporter {
	log := logp.L()
	log.Debugf("Starting metrics logging")
	return LoggerReporter{log: logp.L()}
}

// ReportQueueMetrics satisfies the reporter interface
func (rep LoggerReporter) ReportQueueMetrics(stats reporter.QueueMetrics) error {
	conv, err := toKeyValuePairs(stats)
	if err != nil {
		return fmt.Errorf("error updating metrics: %w", err)
	}
	rep.log.Infow("Queue Statistics", logp.Reflect("queue", conv))
	return nil
}

// Close just satisfies the Reporter Interface
func (rep LoggerReporter) Close() error {
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
