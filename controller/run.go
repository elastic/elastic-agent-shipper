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

	"google.golang.org/grpc/credentials"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
	"github.com/elastic/elastic-agent-shipper/grpcserver"
	"github.com/elastic/elastic-agent-shipper/publisherserver"
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
	runner := publisherserver.NewOutputServer()
	err = runner.Start(cfg)
	if err != nil {
		return err
	}

	srv := grpcserver.NewGRPCServer(runner)

	done := make(doneChan)
	go func() {
		err = srv.Start(creds, cfg.Shipper.Server.Server)
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
	//return nil
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

// runController is the main runloop for the shipper itself, and managed communication with the agent. This is a blocking function
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
			handler.grpcServer.Stop()
			handler.outputHandler.Close()
			return nil
		case <-ctx.Done():
			handler.log.Info("Got context done")
			handler.grpcServer.Stop()
			handler.outputHandler.Close()
			return nil
		case change := <-agentClient.UnitChanges():
			switch change.Type {
			case client.UnitChangedAdded: // The agent is starting the shipper
				handler.handleUnitAdded(change.Unit)
			case client.UnitChangedModified: // config for a unit has changed
				handler.handleUnitUpdated(change.Unit)
			case client.UnitChangedRemoved: // a unit has been stopoped and can now be removed.
				handler.handleUnitRemoved(change.Unit)
			}

		}
	}
}
