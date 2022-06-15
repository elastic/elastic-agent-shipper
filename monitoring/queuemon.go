// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package monitoring

import (
	"fmt"
	"time"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/opt"

	"github.com/elastic/elastic-agent-shipper/monitoring/reporter"
	"github.com/elastic/elastic-agent-shipper/queue"

	"github.com/elastic/elastic-agent-shipper/monitoring/reporter/expvar"
	"github.com/elastic/elastic-agent-shipper/monitoring/reporter/log"
)

//QueueMonitor is the main handler object for the queue monitor, and will be responsible for startup, shutdown, handling config, and persistent tracking of metrics.
type QueueMonitor struct {
	// reporters is an array of user-configured reporters
	reporters []reporter.Reporter
	// interval is the user-configured reporting interval
	interval time.Duration
	done     chan struct{}
	// handler for the event queue
	target queue.MetricsSource
	log    *logp.Logger
	// enabled is a awkward no-op if a user has disabled monitoring
	enabled bool

	// Count of times the queue has reached a configured limit.
	queueLimitCount uint64
}

//Config is the intermediate struct representation of the queue monitor config
type Config struct {
	LogOutput    bool          `config:"logs"`
	ExpvarOutput expvar.Config `config:"http"`
	Interval     time.Duration `config:"interval"`
	Enabled      bool          `config:"enabled"`
}

// DefaultConfig returns the default settings for the queue monitor
func DefaultConfig() Config {
	return Config{
		ExpvarOutput: expvar.Config{
			Enabled: false,
			Port:    8080,
			Host:    "localhost",
			Name:    "queue",
		},
		LogOutput: true,
		Enabled:   true,
		Interval:  time.Second * 30,
	}
}

// NewFromConfig creates a new queue monitor from a pre-filled config struct.
func NewFromConfig(cfg Config, target queue.MetricsSource) (*QueueMonitor, error) {
	// the queue == nil is largely a shim to make things not panic while we wait for the queues to get hooked up.
	if !cfg.Enabled || target == nil {
		return &QueueMonitor{enabled: true}, nil
	}
	//init reporters
	reporters := initReporters(cfg)
	return &QueueMonitor{
		interval:  cfg.Interval,
		target:    target,
		done:      make(chan struct{}),
		log:       logp.L(),
		reporters: reporters,
	}, nil
}

// Watch is a non-blocking call that starts up a queue watcher that will report metrics to a given output
func (mon QueueMonitor) Watch() {
	// Turn this function into a no-op if nothing is initialized.
	if mon.enabled {
		return
	}
	ticker := time.NewTicker(mon.interval)
	go func() {
		for {
			select {
			case <-mon.done:
				return
			case <-ticker.C:
				//We're assuming that the `Metrics()` call from the queue won't hard-block.
				err := mon.updateMetrics()
				if err != nil {
					mon.log.Errorf("Error updating metrics: %s", err)
				}
			}
		}
	}()
}

// End closes the metrics reporter and associated interfaces.
func (mon QueueMonitor) End() {
	if mon.enabled {
		return
	}
	mon.log.Infof("Shutting down metrics monitor...")
	for _, out := range mon.reporters {
		out.Close()
	}
	mon.done <- struct{}{}
}

// updateMetrics is responsible for fetching the metrics from the queue, calculating whatever it needs to, and sending the complete events to the output
func (mon *QueueMonitor) updateMetrics() error {
	raw, err := mon.target.Metrics()
	if err != nil {
		return fmt.Errorf("error fetching queue Metrics: %w", err)
	}

	count, limit, queueIsFull, err := getLimits(raw)
	if err != nil {
		return fmt.Errorf("could not get queue metrics limits: %w", err)
	}

	if queueIsFull {
		mon.queueLimitCount = mon.queueLimitCount + 1
	}

	mon.sendToReporters(reporter.QueueMetrics{
		CurrentLevel:      opt.UintWith(count),
		MaxLevel:          opt.UintWith(limit),
		IsFull:            queueIsFull,
		LimitReachedCount: opt.UintWith(mon.queueLimitCount),
		UnackedRead:       raw.UnackedConsumedEvents,
		// Running on a philosophy that the outputs should be dumb and unopinionated,
		//so we're doing the type conversion here.
		OldestActiveTimestamp: raw.OldestActiveTimestamp.String(),
	})

	return nil
}

func (mon QueueMonitor) sendToReporters(metrics reporter.QueueMetrics) {
	for _, out := range mon.reporters {
		err := out.ReportQueueMetrics(metrics)
		//Assuming we don't want to make this a hard error, since one broken output doesn't mean they're all broken.
		if err != nil {
			mon.log.Errorf("Error sending to output: %w", err)
		}
	}
}

// Load the raw config and look for monitoring outputs to initialize.
func initReporters(cfg Config) []reporter.Reporter {
	outReporters := []reporter.Reporter{}

	if cfg.LogOutput {
		reporter := log.NewLoggerReporter()
		outReporters = append(outReporters, reporter)
	}

	if cfg.ExpvarOutput.Enabled {
		reporter := expvar.NewExpvarReporter(cfg.ExpvarOutput)
		outReporters = append(outReporters, reporter)
	}

	return outReporters
}

// This is a wrapper to deal with the multiple queue metric "types",
// as we could either be dealing with event counts, or bytes.
// The reporting interfaces assumes we only want one.
func getLimits(raw queue.Metrics) (uint64, uint64, bool, error) {

	//bias towards byte count, as it's a little more granular.
	if raw.ByteCount.Exists() && raw.ByteLimit.Exists() {
		count := raw.ByteCount.ValueOr(0)
		limit := raw.ByteLimit.ValueOr(0)
		// As @faec has noted, calculating limits can be a bit awkward when we're dealing with reporting/configuration in bytes.
		//All we have now is total queue size in bytes, which doesn't tell us how many more events could fit before we hit the queue limit.
		//So until we have something better, mark anything as 90% or more full as "full"

		// I'm assuming that limit can be zero here, as perhaps a user can configure a queue without a limit, and it gets passed down to us.
		if limit == 0 {
			return count, limit, false, nil
		}
		level := float64(count) / float64(limit)
		return count, limit, level > 0.9, nil
	}

	if raw.EventCount.Exists() && raw.EventLimit.Exists() {
		count := raw.EventCount.ValueOr(0)
		limit := raw.EventLimit.ValueOr(0)
		return count, limit, count >= limit, nil
	}

	return 0, 0, false, fmt.Errorf("could not find valid byte or event metrics in queue")
}
