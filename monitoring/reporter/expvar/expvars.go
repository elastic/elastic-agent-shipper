// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package expvar

import (
	"expvar"
	"net/http"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
	"github.com/elastic/elastic-agent-libs/transform/typeconv"
	"github.com/elastic/elastic-agent-shipper/monitoring/reporter"
)

//ExpvarsConfig is the config struct for marshalling whatever we get from the config file
type ExpvarsConfig struct {
	Enabled bool   `config:"enabled"`
	Addr    string `config:"address"`
	Name    string `config:"name"`
}

// Expvars is the simple manager for the expvars web interface
type Expvars struct {
	log     *logp.Logger
	metrics reporter.QueueMetrics
}

// NewExpvarReporter initializes the expvar interface, and starts the http frontend.
func NewExpvarReporter(cfg ExpvarsConfig) reporter.Reporter {
	exp := Expvars{
		log: logp.L(),

		metrics: reporter.QueueMetrics{},
	}
	exp.log.Debugf("Starting expvar monitoring...")
	expvar.Publish(cfg.Name, expvar.Func(exp.format))
	exp.runFrontend(cfg)
	return &exp
}

func (exp Expvars) runFrontend(cfg ExpvarsConfig) {
	go func() {
		err := http.ListenAndServe(cfg.Addr, nil)
		if err != nil {
			// Error type isn't happy with %w here
			exp.log.Errorf("Error starting HTTP expvar server: %s", err)
		}
	}()
}

func (exp *Expvars) format() interface{} {
	to := mapstr.M{}

	err := typeconv.Convert(&to, exp.metrics)
	// This function is a callback sent to expvar.Func, so this is probably the best way to handle errors.
	if err != nil {
		exp.log.Errorf("Error formatting queue metrics: %w", err)
	}

	return to
}

// ReportQueueMetrics updates the queue metrics in the output
func (exp *Expvars) ReportQueueMetrics(queue reporter.QueueMetrics) error {
	exp.metrics = queue

	return nil
}
