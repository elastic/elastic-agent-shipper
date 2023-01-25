// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-libs/logp"
	pb "github.com/elastic/elastic-agent-shipper-client/pkg/proto"

	"github.com/elastic/elastic-agent-shipper/config"
	"github.com/elastic/elastic-agent-shipper/monitoring"
	"github.com/elastic/elastic-agent-shipper/output"
	"github.com/elastic/elastic-agent-shipper/output/elasticsearch"
	"github.com/elastic/elastic-agent-shipper/output/kafka"
	"github.com/elastic/elastic-agent-shipper/output/logstash"
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
// under V2, this object will assume control of any unit state changes that happen internally,
// and will be responsible for marking the shipper's units as failed, started, etc
// This is because the input and output unit will map to different internal components; queue/output for the Output unit, gRPC for tine Input unit.
type ServerRunner struct {
	log *logp.Logger
	cfg config.ShipperRootConfig
	// to protect the `Close` function from multiple concurrent calls,
	// so we avoid race conditions
	closeOnce sync.Once
	// to make sure that `Close` is not called before `Start` fully finished
	startMutex sync.Mutex

	server     *grpc.Server
	shipper    server.ShipperServer
	queue      *queue.Queue
	monitoring *monitoring.QueueMonitor
	out        Output

	// units for reporting state
	// In the future, we'll have a dedicated input unit for GRPC
	// Right now, the mapping is:
	// output unit -> queue/output state
	// input unit -> gRPC state
	outUnit *client.Unit
	inUnit  *client.Unit
}

// NewOutputServer initializes the shipper queue and output based on the config received
// errors and state will be sent upstream based on if the error was in an input or output subsystem.
func NewOutputServer(cfg config.ShipperRootConfig, grpcTLS credentials.TransportCredentials, outUnit *client.Unit, inUnit *client.Unit) (r *ServerRunner, err error) {
	r = &ServerRunner{
		log:     logp.L(),
		cfg:     cfg,
		outUnit: outUnit,
		inUnit:  inUnit,
	}

	r.log.Debugf("initializing the queue...")
	r.queue, err = queue.New(cfg.Shipper.Queue)
	if err != nil {
		msg := fmt.Errorf("couldn't create queue: %w", err)
		r.reportState(r.outUnit, msg.Error(), client.UnitStateFailed)
		return nil, msg
	}

	r.reportState(r.outUnit, "queue initialized.", client.UnitStateStarting)

	r.log.Debugf("initializing monitoring ...")

	r.monitoring, err = monitoring.NewFromConfig(cfg.Shipper.Monitor, r.queue)
	if err != nil {
		msg := fmt.Errorf("error initializing output monitor: %w", err)
		r.reportState(r.outUnit, msg.Error(), client.UnitStateFailed)
		return nil, msg
	}
	r.monitoring.Watch()

	r.reportState(r.outUnit, "monitoring is ready.", client.UnitStateStarting)

	r.log.Debugf("initializing the output...")

	r.out, err = outputFromConfig(cfg.Shipper.Output, r.queue)
	if err != nil {
		msg := fmt.Errorf("error generating output config: %w", err)
		r.reportState(r.outUnit, msg.Error(), client.UnitStateFailed)
		return nil, msg
	}
	err = r.out.Start()
	if err != nil {
		msg := fmt.Errorf("couldn't start output: %w", err)
		r.reportState(r.outUnit, msg.Error(), client.UnitStateFailed)
		return nil, msg
	}

	r.reportState(r.outUnit, "output was initialized.", client.UnitStateStarting)

	r.log.Debugf("initializing the gRPC server...")
	opts := []grpc.ServerOption{grpc.Creds(grpcTLS)}
	r.server = grpc.NewServer(opts...)
	r.shipper, err = server.NewShipperServer(cfg.Shipper.StrictMode, r.queue)
	if err != nil {
		msg := fmt.Errorf("could not initialize gRPC: %w", err)
		r.reportState(r.inUnit, msg.Error(), client.UnitStateFailed)
		return nil, msg
	}

	pb.RegisterProducerServer(r.server, r.shipper)
	r.reportState(r.inUnit, "gRPC server initialized.", client.UnitStateStarting)

	return r, nil
}

// Start runs the shipper server according to the set configuration.
// The server stops after calling `Close`, this function blocks until then.
// will report failures and state upstream to its assigned input unit
func (r *ServerRunner) Start() error {
	r.startMutex.Lock()
	if r.server == nil {
		r.startMutex.Unlock()
		msg := fmt.Errorf("failed to start a server runner that was previously closed")
		r.reportState(r.inUnit, msg.Error(), client.UnitStateFailed)
		return msg
	}

	listenSocket := strings.TrimPrefix(r.cfg.Shipper.Server.Server, "unix://")
	// paranoid checking, make sure we have the base directory.
	dir := filepath.Dir(listenSocket)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			r.startMutex.Unlock()
			msg := fmt.Errorf("could not create directory for unix socket %s: %w", dir, err)
			r.reportState(r.inUnit, msg.Error(), client.UnitStateFailed)
			return msg
		}
	}

	lis, err := net.Listen("unix", listenSocket)
	if err != nil {
		r.startMutex.Unlock()
		msg := fmt.Errorf("failed to listen on %s: %w", listenSocket, err)
		r.reportState(r.inUnit, msg.Error(), client.UnitStateFailed)
		return msg
	}

	var wg errgroup.Group
	wg.Go(func() error {
		return r.server.Serve(lis)
	})

	// Testing that the server is running and only then unlock the mutex.
	// Otherwise if `Close` is called at the same time as `Start` it causes race condition.
	wg.Go(func() error {
		defer r.startMutex.Unlock()
		con, err := net.Dial("unix", listenSocket)
		if err != nil {
			err = fmt.Errorf("failed to test connection with the gRPC server on %s: %w", listenSocket, err)
			r.reportState(r.inUnit, err.Error(), client.UnitStateFailed)
			// this will stop the other go routine in the wait group
			r.server.Stop()
			return err
		}
		_ = con.Close()
		r.reportState(r.inUnit, "gRPC server is ready and is listening", client.UnitStateHealthy)
		// not sure if we have a better place to mark the output components as healthy
		r.reportState(r.outUnit, "shipper server is ready and is listening", client.UnitStateHealthy)
		return nil
	})

	err = wg.Wait()
	if err != nil {
		err = fmt.Errorf("error in shipper server: %w", err)
		r.reportState(r.inUnit, err.Error(), client.UnitStateFailed)
		return err
	}
	return nil
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

		// shipper and server map to the input units
		if r.shipper != nil {
			r.reportState(r.inUnit, "shipper is shutting down...", client.UnitStateStopping)
			err = r.shipper.Close()
			if err != nil {
				r.log.Error(err)
			}
			r.shipper = nil
			r.reportState(r.inUnit, "shipper is stopped", client.UnitStateStopping)
		}
		if r.server != nil {
			r.reportState(r.inUnit, "gRPC server is shutting down...", client.UnitStateStopping)
			r.server.GracefulStop()
			r.server = nil
			r.reportState(r.inUnit, "gRPC server is stopped, all connections closed.", client.UnitStateStopping)
		}

		r.reportState(r.inUnit, "input stopped", client.UnitStateStopped)

		// the rest are mapped to the output unit
		if r.monitoring != nil {
			r.reportState(r.outUnit, "monitoring is shutting down...", client.UnitStateStopping)
			r.monitoring.End()
			r.monitoring = nil
			r.reportState(r.outUnit, "monitoring has stopped", client.UnitStateStopping)
		}
		if r.queue != nil {
			r.reportState(r.outUnit, "queue is shutting down...", client.UnitStateStopping)
			err := r.queue.Close()
			if err != nil {
				r.log.Error(err)
			}
			r.queue = nil
			r.reportState(r.outUnit, "queue has stoppped", client.UnitStateStopping)
		}
		if r.out != nil {
			// The output will shut down once the queue is closed.
			// We call Wait to give it a chance to finish with events
			// it has already read.
			r.reportState(r.outUnit, "output is stopping...", client.UnitStateStopping)
			r.out.Wait()
			r.out = nil
			r.reportState(r.outUnit, "all pending events are flushed", client.UnitStateStopping)
		}
		r.reportState(r.outUnit, "output stopped", client.UnitStateStopped)
	})

	return nil
}

func (r *ServerRunner) reportState(unit *client.Unit, msg string, state client.UnitState) {
	if unit != nil {
		_ = unit.UpdateState(state, msg, nil)
	} else {
		r.log.Debugf("updated state: %s", msg)
	}
}

func outputFromConfig(config output.Config, queue *queue.Queue) (Output, error) {
	if config.Elasticsearch != nil && config.Elasticsearch.Enabled {
		return elasticsearch.NewElasticSearch(config.Elasticsearch, queue), nil
	}
	if config.Kafka != nil && config.Kafka.Enabled {
		return kafka.NewKafka(config.Kafka, queue), nil
	}
	if config.Console != nil && config.Console.Enabled {
		return output.NewConsole(queue), nil
	}
	if config.Logstash != nil && config.Logstash.Enabled {
		return logstash.NewLogstash(config.Logstash, queue), nil
	}
	return nil, errors.New("no active output configuration")
}
