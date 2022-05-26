// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"context"
	"fmt"

	pbts "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/elastic/elastic-agent-libs/logp"
	pb "github.com/elastic/elastic-agent-shipper/api"
	"github.com/elastic/elastic-agent-shipper/queue"
)

type shipperServer struct {
	logger *logp.Logger

	queue *queue.Queue

	pb.UnimplementedProducerServer
}

// PublishEvents is the server implementation of the gRPC PublishEvents call
func (serv shipperServer) PublishEvents(_ context.Context, req *pb.PublishRequest) (*pb.PublishReply, error) {
	results := []*pb.EventResult{}
	for _, evt := range req.Events {
		serv.logger.Infof("Got event %s: %#v", evt.EventId, evt.Fields.AsMap())
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
