// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package publisherserver

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"

	"github.com/elastic/elastic-agent-shipper/config"
	"github.com/elastic/elastic-agent-shipper/monitoring"
	"github.com/elastic/elastic-agent-shipper/output"
	"github.com/elastic/elastic-agent-shipper/output/elasticsearch"
	"github.com/elastic/elastic-agent-shipper/output/kafka"
	"github.com/elastic/elastic-agent-shipper/output/logstash"
	"github.com/elastic/elastic-agent-shipper/queue"
)

// Output describes a typical output implementation used for running the server.
type Output interface {
	Start() error
	Wait()
}

// ServerRunner starts and gracefully stops the shipper server on demand.
type ServerRunner struct {
	log *logp.Logger

	// to make sure we don't shut down or modify the queue while we're accessing it
	queueMutex sync.Mutex

	queue      *queue.Queue
	monitoring *monitoring.QueueMonitor
	out        Output
}

// NewOutputServer initializes the output object
func NewOutputServer() *ServerRunner {
	r := &ServerRunner{
		log:        logp.L(),
		queueMutex: sync.Mutex{},
	}
	return r
}

// Start initializes the shipper server with the provided config.
// This is a non-blocking call
func (r *ServerRunner) Start(cfg config.ShipperRootConfig, reportCallback elasticsearch.WatchReporter) error {
	r.log.Debugf("initializing the queue...")
	var err error
	r.queue, err = queue.New(cfg.Shipper.Queue)
	if err != nil {
		return fmt.Errorf("couldn't create queue: %w", err)

	}

	r.log.Debugf("queue initialized")

	r.log.Debugf("initializing monitoring ...")

	r.monitoring, err = monitoring.NewFromConfig(cfg.Monitoring, cfg.LogMetrics, r.queue)
	if err != nil {
		return fmt.Errorf("error initializing output monitor: %w", err)
	}

	r.log.Debugf("monitoring is ready")

	r.log.Debugf("initializing the output...")

	r.out, err = outputFromConfig(cfg.Shipper.Output, r.queue, reportCallback)
	if err != nil {
		return fmt.Errorf("error generating output config: %w", err)
	}
	r.log.Debugf("output is initialized")
	err = r.out.Start()
	if err != nil {
		return fmt.Errorf("couldn't start output: %w", err)
	}
	r.log.Debugf("output is started")

	return nil
}

// QueueDiagCallback returns
func (r *ServerRunner) QueueDiagCallback() client.DiagnosticHook {
	if r.monitoring != nil {
		return r.monitoring.DiagnosticsCallback()
	}
	return func() []byte {
		return []byte("queue monitoring has not started")
	}
}

// Close shuts the whole shipper server down. Can be called only once, following calls are noop.
func (r *ServerRunner) Close() (err error) {
	r.queueMutex.Lock()
	defer r.queueMutex.Unlock()
	// the rest are mapped to the output unit
	if r.monitoring != nil {
		r.log.Debugf("monitoring is shutting down...")
		r.monitoring.End()
		r.monitoring = nil
		r.log.Debugf("monitoring has stopped")
	}
	if r.queue != nil {
		r.log.Debugf("queue is stopping...")
		err := r.queue.Close()
		if err != nil {
			r.log.Error(err)
		}
		r.queue = nil
		r.log.Debugf("queue has stopped")
	}
	if r.out != nil {
		// The output will shut down once the queue is closed.
		// We call Wait to give it a chance to finish with events
		// it has already read.
		r.log.Debugf("output is shutting down...")
		r.out.Wait()
		r.out = nil
		r.log.Debugf("all pending events are flushed")
	}
	r.log.Debugf("output has stopped")

	return nil
}

// IsInitialized returns true if the publisher is attached to a queue and running
func (r *ServerRunner) IsInitialized() bool {
	r.queueMutex.Lock()
	defer r.queueMutex.Unlock()
	return r.queue != nil
}

// PersistedIndex wraps the queue's PersistedIndex call
func (r *ServerRunner) PersistedIndex() (queue.EntryID, error) {
	r.queueMutex.Lock()
	defer r.queueMutex.Unlock()
	if r.queue == nil {
		return 0, fmt.Errorf("shipper is initializing")
	}
	return r.queue.PersistedIndex()
}

// Publish wraps the queue's Publish call
func (r *ServerRunner) Publish(ctx context.Context, event *messages.Event) (queue.EntryID, error) {
	r.queueMutex.Lock()
	defer r.queueMutex.Unlock()
	if r.queue == nil {
		return 0, fmt.Errorf("shipper is initializing")
	}
	return r.queue.Publish(ctx, event)
}

// TryPublish wraps the queue's TryPublish call
func (r *ServerRunner) TryPublish(event *messages.Event) (queue.EntryID, error) {
	r.queueMutex.Lock()
	defer r.queueMutex.Unlock()
	if r.queue == nil {
		return 0, fmt.Errorf("shipper is initializing")
	}
	return r.queue.TryPublish(event)
}

func outputFromConfig(config output.Config, queue *queue.Queue, reporter elasticsearch.WatchReporter) (Output, error) {
	if config.Elasticsearch != nil {
		return elasticsearch.NewElasticSearch(config.Elasticsearch, reporter, queue), nil
	}
	if config.Kafka != nil {
		return kafka.NewKafka(config.Kafka, queue), nil
	}
	if config.Console != nil {
		return output.NewConsole(queue), nil
	}
	if config.Logstash != nil {
		return logstash.NewLogstash(config.Logstash, queue), nil
	}
	return nil, errors.New("no active output configuration")
}
