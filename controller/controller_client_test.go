// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build !integration

package controller

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
	"github.com/elastic/elastic-agent-libs/logp"
)

func TestAgentControl(t *testing.T) {
	unitOutputID := fmt.Sprintf("%s-output", mock.NewID())
	unitInputID := fmt.Sprintf("%s-input", mock.NewID())

	token := mock.NewID()
	var gotConfig, gotHealthy, gotStopped bool

	var mut sync.Mutex
	_ = logp.DevelopmentSetup()

	doneWaiter := sync.WaitGroup{}
	t.Logf("Creating mock server")
	srv := mock.StubServerV2{
		CheckinV2Impl: func(observed *proto.CheckinObserved) *proto.CheckinExpected {
			mut.Lock()
			defer mut.Unlock()
			if observed.Token == token {
				if unitsAreFailed(observed.Units) {
					t.Logf("Got failed unit")
					t.FailNow()
				}
				if gotStopped {
					doneWaiter.Done()
				}
				// initial checkin
				if len(observed.Units) == 0 || observed.Units[0].State == proto.State_STARTING {
					t.Logf("starting initial checkin")
					gotConfig = true
					return &proto.CheckinExpected{
						Units: []*proto.UnitExpected{
							{
								Id:             unitOutputID,
								Type:           proto.UnitType_OUTPUT,
								ConfigStateIdx: 1,
								State:          proto.State_HEALTHY,
								LogLevel:       proto.UnitLogLevel_DEBUG,
								Config: &proto.UnitExpectedConfig{
									Source: MustNewStruct(t, map[string]interface{}{
										"logging": map[string]interface{}{
											"level": "debug",
										},
										"type":    "console",
										"enabled": "true",
									}),
								},
							},
							{
								Id:             unitInputID,
								Type:           proto.UnitType_INPUT,
								ConfigStateIdx: 1,
								State:          proto.State_HEALTHY,
								LogLevel:       proto.UnitLogLevel_DEBUG,
								Config: &proto.UnitExpectedConfig{
									Source: MustNewStruct(t, map[string]interface{}{
										"logging": map[string]interface{}{
											"level": "debug",
										},
										"server": fmt.Sprintf("/tmp/%s.sock", mock.NewID()),
									},
									),
								},
							},
						},
					}
				} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) {
					t.Logf("Got unit state healthy, sending STOPPED")
					gotHealthy = true
					//shutdown
					return &proto.CheckinExpected{
						Units: []*proto.UnitExpected{
							{
								Id:             unitOutputID,
								Type:           proto.UnitType_OUTPUT,
								ConfigStateIdx: 2,
								LogLevel:       proto.UnitLogLevel_DEBUG,
								State:          proto.State_STOPPED,
								Config:         &proto.UnitExpectedConfig{},
							},
							{
								Id:             unitInputID,
								Type:           proto.UnitType_INPUT,
								ConfigStateIdx: 2,
								State:          proto.State_STOPPED,
								LogLevel:       proto.UnitLogLevel_DEBUG,
								Config:         &proto.UnitExpectedConfig{},
							},
						},
					}
				} else if unitsAreState(t, proto.State_STOPPED, observed.Units) {
					gotStopped = true
					t.Logf("Got unit state STOPPED, removing")
					return &proto.CheckinExpected{
						Units: []*proto.UnitExpected{},
					}
				} else {
					for _, unit := range observed.Units {
						t.Logf("current state for %s is: %s", unit.Id, unit.Message)
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
	doneWaiter.Add(1)
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
	go func() {
		doneWaiter.Wait()
		cancel()
	}()

	err := runController(ctx, validClient)
	assert.NoError(t, err)

	assert.True(t, gotConfig, "config state")
	assert.True(t, gotHealthy, "healthy state")
	assert.True(t, gotStopped, "stopped state")
}

func unitsAreState(t *testing.T, expected proto.State, units []*proto.UnitObserved) bool {
	for _, unit := range units {
		if unit.State != expected {
			t.Logf("unit %s was not state %s", unit.Id, expected.String())
			return false
		}
	}
	return true
}

func unitsAreFailed(units []*proto.UnitObserved) bool {
	for _, unit := range units {
		if unit.State == proto.State_FAILED {
			return true
		}
	}
	return false
}

func MustNewStruct(t *testing.T, contents map[string]interface{}) *structpb.Struct {
	result, err := structpb.NewStruct(contents)
	require.NoError(t, err, "failed to create test struct for contents [%v]: %v", contents, err)
	return result
}
