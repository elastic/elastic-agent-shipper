// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
	"github.com/elastic/elastic-agent-shipper/monitoring"

	pb "github.com/elastic/elastic-agent-shipper/api"
)

// LoadAndRun loads the config object and runs the gRPC server
func LoadAndRun() error {
	cfg, err := config.ReadConfig()
	if err != nil {
		return fmt.Errorf("error reading config: %w", err)

	}

	err = logp.Configure(cfg.Log)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)

	}

	return Run(cfg)
}

func handleShutdown(stopFunc func(), log *logp.Logger) {
	var callback sync.Once

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

		callback.Do(stopFunc)
	}()
}

// Run starts the gRPC server
func Run(cfg config.ShipperConfig) error {
	log := logp.L()
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	// Beats won't call the "New*" functions of a queue directly, but instead fetch a queueFactory from the global registers.
	//However, that requires the publisher/pipeline code as well, and I'm not sure we want that.
	monHandler, err := loadMonitoring(cfg)
	if err != nil {
		return fmt.Errorf("error loading outputs: %w", err)
	}

	log.Debugf("Loaded outputs, waiting....")

	var opts []grpc.ServerOption
	if cfg.TLS {
		creds, err := credentials.NewServerTLSFromFile(cfg.Cert, cfg.Key)
		if err != nil {
			return fmt.Errorf("failed to generate credentials %w", err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}
	grpcServer := grpc.NewServer(opts...)
	r := shipperServer{logger: log}
	pb.RegisterProducerServer(grpcServer, r)

	shutdownFunc := func() {
		grpcServer.GracefulStop()
		monHandler.End()
	}
	handleShutdown(shutdownFunc, log)
	log.Debugf("gRPC server is listening on port %d", cfg.Port)
	return grpcServer.Serve(lis)

}

// Initialize metrics and outputs
func loadMonitoring(cfg config.ShipperConfig) (*monitoring.QueueMonitor, error) {
	//If we had an actual queue hooked up, that would go here
	//queue := NewTestQueue()

	//startup monitor
	//remove the nil in the second argument here when we have an actual queue.
	mon, err := monitoring.NewFromConfig(cfg.Monitor, nil)
	if err != nil {
		return nil, fmt.Errorf("error initializing output monitor: %w", err)
	}

	mon.Watch()

	return mon, nil
}
