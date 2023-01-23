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
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
)

// LoadAndRun loads the config object and runs the gRPC server
func LoadAndRun() error {
	err := setPaths()
	if err != nil {
		return fmt.Errorf("error setting paths from -E overwrites: %w", err)
	}
	err = setLogging()
	if err != nil {
		return fmt.Errorf("error configuring logging: %w", err)
	}
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
func RunUnmanaged(cfg config.ShipperRootConfig) error {
	var creds credentials.TransportCredentials
	var err error
	if cfg.Shipper.Server.TLS.Cert != "" && cfg.Shipper.Server.TLS.Key != "" {
		creds, err = credentials.NewServerTLSFromFile(cfg.Shipper.Server.TLS.Cert, cfg.Shipper.Server.TLS.Key)
		if err != nil {
			return fmt.Errorf("failed to generate credentials %w", err)
		}
	}
	log := logp.L()
	runner, err := NewOutputServer(cfg, creds)
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
func RunManaged(_ config.ShipperRootConfig) error {
	agentClient, _, err := client.NewV2FromReader(os.Stdin, client.VersionInfo{Name: "elastic-agent-shipper", Version: "v2"})
	if err != nil {
		return fmt.Errorf("error reading control config from agent: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = runController(ctx, agentClient)

	return err
}

// runController is the main runloop for the shipper itself, and managed communication with the agent.
func runController(ctx context.Context, agentClient client.V2) error {
	log := logp.L()

	err := agentClient.Start(ctx)
	if err != nil {
		return fmt.Errorf("error starting connection to client")
	}

	log.Debugf("Starting error reporter")
	go reportErrors(ctx, agentClient)

	log.Debugf("Started client, waiting")
	handler := newClientHandler()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// use a timer to "bounce" the shipper based on external changes
	// useful since we require two units for the shipper to actually start, which can arrive at different times.
	// This also makes it a little easier to deal with multple events requiring
	// a (re)start of the shipper backend, in which case we'll only restart once with the latest config.
	bounceTime := 200 * time.Millisecond // give us enough time to get multiple units before the shipper takes any action
	shipperSetTimer := time.NewTimer(bounceTime)
	shipperSetTimer.Stop() // start with the timer off, reset when we get a change

	// receive the units
	for {
		select {
		case gotSignal := <-sigc:
			switch gotSignal {
			case syscall.SIGINT, syscall.SIGTERM:
				handler.log.Info("Received sigterm/sigint, stopping")
			case syscall.SIGHUP:
				handler.log.Info("Received sighup, stopping")
			}
			handler.units.DeleteOutput()
			shipperSetTimer.Reset(bounceTime)
			return nil
		case <-ctx.Done():
			handler.log.Info("Got context done")
			handler.units.DeleteOutput()
			shipperSetTimer.Reset(bounceTime)
			return nil
		case change := <-agentClient.UnitChanges():
			restart := false
			switch change.Type {
			case client.UnitChangedAdded: // The agent is starting the shipper, or we added a new processor
				restart = handler.handleUnitAdded(change.Unit)
			case client.UnitChangedModified: // config for a unit has changed
				restart = handler.handleUnitUpdated(change.Unit)
			case client.UnitChangedRemoved: // a unit has been stopoped and can now be removed.
				restart = handler.handleUnitRemoved(change.Unit)
			}
			// tell the shipper to restart
			if restart {
				shipperSetTimer.Reset(bounceTime)
			}
		case <-shipperSetTimer.C:
			handler.BounceShipper()
		}
	}
}
