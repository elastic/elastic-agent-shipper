// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"context"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	pbts "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-client/v7/pkg/proto"
	"github.com/elastic/elastic-agent-libs/logp"
	pb "github.com/elastic/elastic-agent-shipper/api"
	"github.com/elastic/elastic-agent-shipper/config"
	"github.com/elastic/elastic-agent-shipper/monitoring"
	"github.com/elastic/elastic-agent-shipper/output"
	"github.com/elastic/elastic-agent-shipper/queue"
)

type shipperServer struct {
	log        *logp.Logger
	grpcServer *grpc.Server
	queue      *queue.Queue
	monHandler *monitoring.QueueMonitor
	out        *output.ConsoleOutput
	pb.UnimplementedProducerServer
	// This is a temporary hack (hopefully),
	// expvar keeps a bunch of state globally,
	// and will throw a panic if you try to re-register a name
	// once you've reloaded the config
	monWrap         sync.Once
	serverIsStarted bool
}

func newShipper() *shipperServer {
	return &shipperServer{
		log: logp.L(),
	}
}

// PublishEvents is the server implementation of the gRPC PublishEvents call
func (serv shipperServer) PublishEvents(_ context.Context, req *pb.PublishRequest) (*pb.PublishReply, error) {
	results := []*pb.EventResult{}
	for _, evt := range req.Events {
		serv.log.Infof("Got event %s: %#v", evt.EventId, evt.Fields.AsMap())
		err := serv.queue.Publish(evt)
		if err != nil {
			// If we couldn't accept any events, return the error directly. Otherwise,
			// just return success on however many events we were able to handle.
			if len(results) == 0 {
				return nil, err
			}
			break
		}
		res := pb.EventResult{EventId: evt.EventId, Timestamp: pbts.Now()}
		results = append(results, &res)
	}
	return &pb.PublishReply{Results: results}, nil
}

// StreamAcknowledgements is the server implementation of the gRPC StreamAcknowledgements call
func (serv shipperServer) StreamAcknowledgements(streamReq *pb.StreamAcksRequest, prd pb.Producer_StreamAcknowledgementsServer) error {

	// we have no outputs now, so just send a single dummy event
	evt := pb.StreamAcksReply{Acks: []*pb.Acknowledgement{{Timestamp: pbts.Now(), EventId: streamReq.DataStream.Id}}}
	err := prd.Send(&evt)

	if err != nil {
		return fmt.Errorf("error in StreamAcknowledgements: %w", err)
	}
	return nil
}

// Run is a blocking call that starts the gRPC server
func (s *shipperServer) Run(cfg config.ShipperConfig, agent client.Client) error {
	if s.serverIsStarted {
		return fmt.Errorf("server is already started")
	}

	// When there is queue-specific configuration in ShipperConfig, it should
	// be passed in here.
	queue, err := queue.New()
	if err != nil {
		return fmt.Errorf("couldn't create queue: %w", err)
	}
	s.queue = queue

	s.out = output.NewConsole(s.queue)
	s.out.Start()

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	// Beats won't call the "New*" functions of a queue directly, but instead fetch a queueFactory from the global registers.
	//However, that requires the publisher/pipeline code as well, and I'm not sure we want that.

	// see shipperServer type declaration, this `once` is just to protect expvar for now.
	s.monWrap.Do(
		func() {
			monHandler, err := loadMonitoring(cfg, s.queue)
			if err != nil {
				s.log.Errorf("Error starting monitoring: %w", err)
				return
			}
			s.monHandler = monHandler
		})

	var opts []grpc.ServerOption
	if cfg.TLS {
		creds, err := credentials.NewServerTLSFromFile(cfg.Cert, cfg.Key)
		if err != nil {
			return fmt.Errorf("failed to generate credentials %w", err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}
	s.grpcServer = grpc.NewServer(opts...)

	pb.RegisterProducerServer(s.grpcServer, s)

	agent.Status(proto.StateObserved_HEALTHY, "Started server, grpc listening", nil)
	s.log.Infof("gRPC server is listening on port %d", cfg.Port)
	s.serverIsStarted = true
	return s.grpcServer.Serve(lis)
}

// blocking command to stop all the shipper components
func (s *shipperServer) Stop() {
	if !s.serverIsStarted {
		return
	}
	s.log.Debugf("Stopping shipper server")
	s.grpcServer.GracefulStop()
	s.queue.Close()
	// Don't try to gracefully stop monitoring until we deal with expvar
	//s.monHandler.End()
	// The output will shut down once the queue is closed.
	// We call Wait to give it a chance to finish with events
	// it has already read.
	s.out.Wait()

	s.serverIsStarted = false
}
