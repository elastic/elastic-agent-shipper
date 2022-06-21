// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
	"github.com/elastic/elastic-agent-shipper/monitoring"
	"github.com/elastic/elastic-agent-shipper/queue"
)

type stopFunc func()

// LoadAndRun loads the config object and runs the gRPC server, this is what gets called by the CLI library on start.
func LoadAndRun() error {
	// Read in a bare config that allows us to pull from the -E flags
	cfg, err := config.GetLoggingConfig()
	if err != nil {
		return fmt.Errorf("error reading config: %w", err)
	}

	// globally set the logger
	err = logp.Configure(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	stdinWrapper := func(agentClient client.StateInterface) (client.Client, error) {
		return client.NewFromReader(os.Stdin, agentClient)
	}

	client, err := NewShipperFromClient(stdinWrapper)
	if err != nil {
		return fmt.Errorf("error starting shipper client: %w", err)
	}

	return runAgentClient(context.Background(), client)
}

func runAgentClient(ctx context.Context, ac *AgentClient) error {
	err := ac.StartClient(ctx)
	if err != nil {
		return fmt.Errorf("error starting client: %w", err)
	}
	log := logp.L()
	handleShutdown(ac.OnStop, log)

	for {
		select {
		case <-ctx.Done():
			log.Infof("Got context done, stopping")
			return nil
		case <-ac.stop:
			log.Infof("Got shutdown, stopping")
			return nil
		}
	}
}

func handleShutdown(stop stopFunc, log *logp.Logger) {
	var callback sync.Once
	log.Debugf("registering signal handler")
	// On termination signals, gracefully stop the Beat
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig := <-sigc

		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			log.Debug("Received sigterm/sigint, stopping")
		case syscall.SIGHUP:
			log.Debug("Received sighup, stopping")
		}

		callback.Do(stop)
	}()
}

// Initialize metrics and outputs
func loadMonitoring(cfg config.ShipperConfig, queue *queue.Queue) (*monitoring.QueueMonitor, error) {
	//startup monitor
	mon, err := monitoring.NewFromConfig(cfg.Monitor, queue)
	if err != nil {
		return nil, fmt.Errorf("error initializing output monitor: %w", err)
	}

	mon.Watch()

	return mon, nil
}
