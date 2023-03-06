// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package grpcserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/elastic/elastic-agent-libs/logp"
	pb "github.com/elastic/elastic-agent-shipper-client/pkg/proto"
	"github.com/elastic/elastic-agent-shipper/tools"
)

// InputHandler wraps the input side of the shipper, and is responsible for starting and stopping the gRPC endpoint
type InputHandler struct {
	Shipper    ShipperServer
	publisher  Publisher
	log        *logp.Logger
	server     *grpc.Server
	startMutex sync.Mutex
}

// NewGRPCServer returns a new gRPC handler with a reference to the output publisher
func NewGRPCServer(publisher Publisher) *InputHandler {
	log := logp.NewLogger("input-handler")
	srv := &InputHandler{log: log, publisher: publisher}

	return srv
}

// Start runs the shipper server according to the set configuration. This is a non-blocking call.
func (srv *InputHandler) Start(grpcTLS credentials.TransportCredentials, endpoint string) error {
	// TODO: only needed while we deal with the fact that we have our config coming from multiple input units
	if srv.server != nil {
		srv.log.Debugf("shipper gRPC already started, continuing...")
		return nil
	}
	listenAddr := strings.TrimPrefix(endpoint, "unix://")
	var err error

	srv.log.Debugf("initializing the gRPC server...")
	opts := []grpc.ServerOption{
		grpc.Creds(grpcTLS),
		grpc.MaxRecvMsgSize(64 * (1 << 20)), // Allow RPCs of up to 64MB
	}
	srv.server = grpc.NewServer(opts...)
	srv.Shipper, err = NewShipperServer(srv.publisher)
	if err != nil {
		return fmt.Errorf("could not initialize gRPC: %w", err)
	}

	pb.RegisterProducerServer(srv.server, srv.Shipper)

	srv.startMutex.Lock()

	// paranoid checking, make sure we have the base directory.
	dir := filepath.Dir(listenAddr)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0o755)
		if err != nil {
			srv.startMutex.Unlock()
			return fmt.Errorf("could not create directory for unix socket %s: %w", dir, err)
		}
	}

	lis, err := newListener(srv.log, listenAddr)
	if err != nil {
		srv.startMutex.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
	}

	go func() {
		err = srv.server.Serve(lis)
		if err != nil {
			srv.log.Errorf("gRPC server shut down with error: %s", err)
		}
	}()
	srv.log.Debugf("gRPC started")
	// Testing that the server is running and only then unlock the mutex.
	// Otherwise if `Close` is called at the same time as `Start` it causes race condition.
	defer srv.startMutex.Unlock()
	con, err := tools.DialTestAddr(listenAddr)
	if err != nil {
		// this will stop the other go routine in the wait group
		srv.server.Stop()
		return fmt.Errorf("failed to test connection with the gRPC server on %s: %w", listenAddr, err)
	}
	_ = con.Close()

	return nil
}

// InitHasFailed reports an error elsewhere to the gRPC server
func (srv *InputHandler) InitHasFailed(e string) {
	if srv.Shipper != nil {
		srv.Shipper.SetInitError(e)
	}
}

// Stop stops the shipper gRPC endpoint
func (srv *InputHandler) Stop() {
	srv.startMutex.Lock()
	defer srv.startMutex.Unlock()

	if srv.Shipper != nil {
		err := srv.Shipper.Close()
		if err != nil {
			srv.log.Debugf("Error stopping shipper input: %s", err)
		}
		srv.Shipper = nil
	}
	if srv.server != nil {
		srv.server.GracefulStop()
		srv.server = nil

	}
}
