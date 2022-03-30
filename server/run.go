package server

import (
	"fmt"
	"net"
	"os"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/elastic/elastic-agent-shipper/shipper"
)

// LoadAndRun loads the config object and runs the gRPC server
func LoadAndRun() error {
	cfg, err := config.ReadConfig()
	if err != nil {
		return fmt.Errorf("error reading config: %w", err)

	}

	err = logp.Configure(cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	return Run(cfg)
}

// Run starts the gRPC server
func Run(cfg config.ShipperConfig) error {
	log := logp.L()
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	var opts []grpc.ServerOption
	if cfg.TLS {
		creds, err := credentials.NewServerTLSFromFile(cfg.Cert, cfg.Key)
		if err != nil {
			return fmt.Errorf("Failed to generate credentials %v", err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}
	grpcServer := grpc.NewServer(opts...)
	r := shipperServer{logger: log}
	pb.RegisterProducerServer(grpcServer, r)
	log.Debugf("gRPC server is listening on port %d", cfg.Port)
	return grpcServer.Serve(lis)

}
