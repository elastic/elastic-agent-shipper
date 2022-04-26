// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"fmt"
	"net"

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

// Run starts the gRPC server
func Run(cfg config.ShipperConfig) error {
	log := logp.L()
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	// Beats won't call the "New*" functions of a queue directly, but instead fetch a queueFactory from the global registers.
	//However, that requires the publisher/pipeline code as well, and I'm not sure we want that.
	err = loadOutputs(cfg)
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
	log.Debugf("gRPC server is listening on port %d", cfg.Port)
	return grpcServer.Serve(lis)

}

// Initialize metrics and outputs
func loadOutputs(cfg config.ShipperConfig) error {
	//If we had an actual queue hooked up, that would go here
	//queue := NewTestQueue()
	fmt.Printf("Got monitoring config: %#v\n", cfg.Monitor)
	//startup monitor
	//remove the nil in the second argument here when we have an actual queue.
	mon, err := monitoring.NewFromConfig(cfg.Monitor, nil)
	if err != nil {
		return fmt.Errorf("error initializing output monitor: %w", err)
	}

	mon.Watch()

	return nil
}
