// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/elastic/elastic-agent-libs/logp"
	pb "github.com/elastic/elastic-agent-shipper-client/pkg/proto"

	"github.com/elastic/elastic-agent-shipper/config"
	"github.com/elastic/elastic-agent-shipper/monitoring"
	"github.com/elastic/elastic-agent-shipper/output"
	"github.com/elastic/elastic-agent-shipper/output/kafka"
	"github.com/elastic/elastic-agent-shipper/queue"
	"github.com/elastic/elastic-agent-shipper/server"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Output describes a typical output implementation used for running the server.
type Output interface {
	Start() error
	Wait()
}

// ServerRunner starts and gracefully stops the shipper server on demand.
// The server runner is not re-usable and should be abandoned after `Close` or canceled context.
type ServerRunner struct {
	log *logp.Logger
	cfg config.ShipperConfig
	// to protect the `Close` function from multiple concurrent calls,
	// so we avoid race conditions
	closeOnce sync.Once
	// to make sure that `Close` is not called before `Start` fully finished
	startMutex sync.Mutex

	server     *grpc.Server
	shipper    server.ShipperServer
	queue      *queue.Queue
	monitoring *monitoring.QueueMonitor
	console    Output
	out        Output
}

// NewServerRunner creates a new runner that starts and stops the server.
// Consumers are expected to run `Close` to release all resources created
// by a successful result from this function even without starting the server.
func NewServerRunner(cfg config.ShipperConfig) (r *ServerRunner, err error) {
	r = &ServerRunner{
		log: logp.L(),
		cfg: cfg,
	}
	// in case of an initialization error we must clean up all created resources
	defer func() {
		if err != nil {
			// this will account for partial initialization in case of an error, so there are no leaks
			r.Close()
		}
	}()

	r.log.Debug("initializing the queue...")
	r.queue, err = queue.New(cfg.Queue)
	if err != nil {
		return nil, fmt.Errorf("couldn't create queue: %w", err)
	}
	r.log.Debug("queue was initialized.")

	r.log.Debug("initializing monitoring ...")
	r.monitoring, err = monitoring.NewFromConfig(cfg.Monitor, r.queue)
	if err != nil {
		return nil, fmt.Errorf("error initializing output monitor: %w", err)
	}
	r.monitoring.Watch()
	r.log.Debug("monitoring is ready.")

	r.log.Debug("initializing the output...")

	r.out, err = outputFromConfig(cfg.Output, r.queue)
	if err != nil {
		return nil, err
	}
	err = r.out.Start()
	if err != nil {
		return nil, fmt.Errorf("couldn't start output: %w", err)
	}
	r.log.Debug("output was initialized.")
	// in case of an initialization error we must clean up all created resources
	defer func() {
		if err != nil {
			// this will account for partial initialization in case of an error, so there are no leaks
			r.Close()
		}
	}()

	r.log.Debug("initializing the gRPC server...")
	var opts []grpc.ServerOption
	if cfg.Server.TLS {
		creds, err := credentials.NewServerTLSFromFile(cfg.Server.Cert, cfg.Server.Key)
		if err != nil {
			return nil, fmt.Errorf("failed to generate credentials %w", err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}
	r.server = grpc.NewServer(opts...)
	r.shipper, err = server.NewShipperServer(cfg.Server, r.queue)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise the server: %w", err)
	}
	pb.RegisterProducerServer(r.server, r.shipper)
	r.log.Debug("gRPC server initialized.")

	return r, nil
}

// Start runs the shipper server according to the set configuration.
// The server stops after calling `Close`, this function blocks until then.
func (r *ServerRunner) Start() (err error) {
	r.startMutex.Lock()
	if r.server == nil {
		r.startMutex.Unlock()
		return fmt.Errorf("failed to start a server runner that was previously closed")
	}

	addr := fmt.Sprintf("localhost:%d", r.cfg.Server.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		r.startMutex.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	var wg errgroup.Group
	wg.Go(func() error {
		return r.server.Serve(lis)
	})

	// Testing that the server is running and only then unlock the mutex.
	// Otherwise if `Close` is called at the same time as `Start` it causes race condition.
	wg.Go(func() error {
		defer r.startMutex.Unlock()
		con, err := net.Dial("tcp", addr)
		if err != nil {
			err = fmt.Errorf("failed to test connection with the gRPC server on %s: %w", addr, err)
			r.log.Error(err)
			// this will stop the other go routine in the wait group
			r.server.Stop()
			return err
		}
		_ = con.Close()
		r.log.Debugf("gRPC server is ready and is listening on %s", addr)
		return nil
	})

	return wg.Wait()
}

// Close shuts the whole shipper server down. Can be called only once, following calls are noop.
func (r *ServerRunner) Close() (err error) {
	r.startMutex.Lock()
	defer r.startMutex.Unlock()
	// initialization can fail on each step, so it's possible that the runner
	// is partially initialized and we have to account for that.
	r.closeOnce.Do(func() {
		// we must stop the shipper first which is closing index subscriptions
		// otherwise `GracefulStop` will hang forever
		if r.shipper != nil {
			r.log.Debugf("shipper is shutting down...")
			err = r.shipper.Close()
			if err != nil {
				r.log.Error(err)
			}
			r.shipper = nil
			r.log.Debugf("shipper is stopped.")
		}
		if r.server != nil {
			r.log.Debugf("gRPC server is shutting down...")
			r.server.GracefulStop()
			r.server = nil
			r.log.Debugf("gRPC server is stopped, all connections closed.")
		}
		if r.monitoring != nil {
			r.log.Debugf("monitoring is shutting down...")
			r.monitoring.End()
			r.monitoring = nil
			r.log.Debugf("monitoring is stopped.")
		}
		if r.queue != nil {
			r.log.Debugf("queue is shutting down...")
			err := r.queue.Close()
			if err != nil {
				r.log.Error(err)
			}
			r.queue = nil
			r.log.Debugf("queue is stopped.")
		}
		if r.out != nil {
			// The output will shut down once the queue is closed.
			// We call Wait to give it a chance to finish with events
			// it has already read.
			r.log.Debugf("waiting for pending events in the output...")
			r.out.Wait()
			r.out = nil
			r.log.Debugf("all pending events are flushed")
		}
	})

	return nil
}

func outputFromConfig(config output.Config, queue *queue.Queue) (Output, error) {
	if config.Kafka != nil {
		return kafka.NewKafka(config.Kafka, queue), nil
	}
	if config.Console != nil && config.Console.Enabled {
		return output.NewConsole(queue), nil
	}
	return nil, errors.New("no active output configuration")
}