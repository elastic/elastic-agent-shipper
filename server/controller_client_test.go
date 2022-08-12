// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	structpb "google.golang.org/protobuf/types/known/structpb"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-client/v7/pkg/client/mock"
	"github.com/elastic/elastic-agent-client/v7/pkg/proto"
)

func MustNewStruct(contents map[string]interface{}) *structpb.Struct {
	result, err := structpb.NewStruct(contents)
	if err != nil {
		panic(fmt.Errorf("failed to create test struct for contents: %w", err))
	}
	return result
}

func TestAgentControl(t *testing.T) {
	unitOneID := mock.NewID()

	token := mock.NewID()
	var gotConfig, gotHealthy, gotStopped bool

	var mut sync.Mutex

	t.Logf("Creating mock server")
	srv := mock.StubServerV2{
		CheckinV2Impl: func(observed *proto.CheckinObserved) *proto.CheckinExpected {
			mut.Lock()
			defer mut.Unlock()
			if observed.Token == token {
				if len(observed.Units) > 0 {
					t.Logf("Current unit state is: %v", observed.Units[0].State)
				}

				// initial checkin
				if len(observed.Units) == 0 || observed.Units[0].State == proto.State_STARTING {
					gotConfig = true
					return &proto.CheckinExpected{
						Units: []*proto.UnitExpected{
							{
								Id:             unitOneID,
								Type:           proto.UnitType_OUTPUT,
								ConfigStateIdx: 1,
								Config: &proto.UnitExpectedConfig{
									Id: "config_unit_one",
									Source: MustNewStruct(map[string]interface{}{
										"logging": map[string]interface{}{"level": "debug"},
									}),
								},
								State: proto.State_HEALTHY,
							},
						},
					}
				} else if observed.Units[0].State == proto.State_HEALTHY {
					gotHealthy = true
					//shutdown
					return &proto.CheckinExpected{
						Units: []*proto.UnitExpected{
							{
								Id:             unitOneID,
								Type:           proto.UnitType_OUTPUT,
								ConfigStateIdx: 1,
								//Config:         "{}",
								State: proto.State_STOPPED,
							},
						},
					}
				} else if observed.Units[0].State == proto.State_STOPPED {
					gotStopped = true
					// remove the unit? I think?
					return &proto.CheckinExpected{
						Units: nil,
					}
				}

			}

			//gotInvalid = true
			return nil
		},
		ActionImpl: func(response *proto.ActionResponse) error {

			return nil
		},
		ActionsChan: make(chan *mock.PerformAction, 100),
	} // end of srv declaration

	require.NoError(t, srv.Start())
	defer srv.Stop()

	t.Logf("creating client")
	// connect with client
	validClient := client.NewV2(fmt.Sprintf(":%d", srv.Port), token, client.VersionInfo{
		Name:    "program",
		Version: "v1.0.0",
		Meta: map[string]string{
			"key": "value",
		},
	}, grpc.WithTransportCredentials(insecure.NewCredentials()))

	t.Logf("starting shipper controller")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	err := runController(ctx, validClient)
	assert.NoError(t, err)

	assert.True(t, gotConfig, "config state")
	assert.True(t, gotHealthy, "healthy state")
	assert.True(t, gotStopped, "stopped state")
}
