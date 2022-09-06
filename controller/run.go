// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
)

// LoadAndRun loads the config object and runs the gRPC server
func LoadAndRun() error {
	cfg, err := config.ReadConfigFromFile()
	switch {
	case err == nil:
		return RunUnmanaged(cfg)
	case errors.Is(err, config.ErrConfigIsNotSet):
		return RunManaged(cfg)
	default:
		return err
	}
}

// RunUnmanaged runs the shipper out of a local config file without using the control protocol.
func RunUnmanaged(cfg config.ShipperConfig) error {
	log := logp.L()
	runner, err := NewServerRunner(cfg)
	if err != nil {
		return err
	}
	done := make(doneChan)
	go func() {
		err = runner.Start()
		done <- struct{}{}
	}()

	// On termination signals, gracefully stop the shipper
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	sig := <-sigc
	switch sig {
	case syscall.SIGINT, syscall.SIGTERM:
		log.Debug("Received sigterm/sigint, stopping")
	case syscall.SIGHUP:
		log.Debug("Received sighup, stopping")
	}

	_ = runner.Close()

	<-done

	return err
}

// RunManaged runs the shipper receiving configuration from the agent using the control protocol.
func RunManaged(cfg config.ShipperConfig) error {
	agentClient, _, err := client.NewV2FromReader(os.Stdin, client.VersionInfo{Name: "elastic-agent-shipper", Version: "v2"})
	if err != nil {
		return fmt.Errorf("error reading control config from agent: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = runController(ctx, agentClient)

	return err
}
