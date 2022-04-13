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
