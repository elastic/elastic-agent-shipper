package server

import (
	"context"
	"fmt"

	"github.com/elastic/elastic-agent-libs/logp"
	pb "github.com/elastic/elastic-agent-shipper/shipper"
	pbts "google.golang.org/protobuf/types/known/timestamppb"
)

type shipperServer struct {
	logger *logp.Logger
	pb.UnimplementedProducerServer
}

// PublishEvents is the server implementation of the gRPC PublishEvents call
func (serv shipperServer) PublishEvents(_ context.Context, req *pb.PublishRequest) (*pb.PublishReply, error) {
	results := []*pb.EventResult{}
	for _, evt := range req.Events {
		serv.logger.Infof("Got event %s: %#v", evt.EventId, evt.Fields.AsMap())
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
		return fmt.Errorf("Error in StreamAcknowledgements: %w", err)
	}
	return nil
}
