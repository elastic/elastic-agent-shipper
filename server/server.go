// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"context"

	"github.com/elastic/elastic-agent-libs/logp"
	pb "github.com/elastic/elastic-agent-shipper-client/pkg/proto"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/queue"
)

type shipperServer struct {
	logger *logp.Logger

	queue *queue.Queue

	pb.UnimplementedProducerServer
}

// PublishEvents is the server implementation of the gRPC PublishEvents call
func (serv shipperServer) PublishEvents(_ context.Context, req *messages.PublishRequest) (*messages.PublishReply, error) {
	reply := &messages.PublishReply{}
	for _, evt := range req.Events {
		serv.logger.Infof("Got event: %s", evt.String())
		entryID, err := serv.queue.Publish(evt)
		if err != nil {
			// If we couldn't accept any events, return the error directly. Otherwise,
			// just return success on however many events we were able to handle.
			if reply.AcceptedCount == 0 {
				return nil, err
			}
			break
		}
		reply.AcceptedCount = reply.AcceptedCount + 1
		reply.AcceptedIndex = uint64(entryID)
	}
	return reply, nil
}

// StreamAcknowledgements is the server implementation of the gRPC StreamAcknowledgements call
// func (serv shipperServer) StreamAcknowledgements(streamReq *messages.StreamAcksRequest, prd pb.Producer_StreamAcknowledgementsServer) error {

// 	// we have no outputs now, so just send a single dummy event
// 	evt := messages.StreamAcksReply{Acks: []*messages.Acknowledgement{{Timestamp: pbts.Now(), EventId: streamReq.Source.GetInputId()}}}
// 	err := prd.Send(&evt)

// 	if err != nil {
// 		return fmt.Errorf("error in StreamAcknowledgements: %w", err)
// 	}
// 	return nil
// }
