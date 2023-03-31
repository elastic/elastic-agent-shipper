// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build !integration && !windows

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	structpb "google.golang.org/protobuf/types/known/structpb"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-client/v7/pkg/client/mock"
	"github.com/elastic/elastic-agent-client/v7/pkg/proto"
	"github.com/elastic/elastic-agent-libs/logp"
)

func TestRemoveOutput(t *testing.T) {
	token := mock.NewID()
	var gotConfig, stoppingOutput, restartedOutput, outputHealthy, testStopped bool

	var mut sync.Mutex
	_ = logp.DevelopmentSetup()

	unitOut, unitIn := basicStartingUnits(t)
	doneWaiter := &sync.WaitGroup{}
	srvFunc := func(observed *proto.CheckinObserved) *proto.CheckinExpected {
		mut.Lock()
		defer mut.Unlock()
		if observed.Token == token {
			if unitsAreFailed(observed.Units) {
				t.Logf("Got failed unit")
				t.FailNow()
			}
			if testStopped {
				return nil
			}
			// initial checkin
			if len(observed.Units) == 0 || observed.Units[0].State == proto.State_STARTING {
				t.Logf("starting initial checkin")
				gotConfig = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && !restartedOutput {
				t.Logf("Got unit state healthy, stopping output")
				stoppingOutput = true

				unitOut.ConfigStateIdx++
				unitOut.State = proto.State_STOPPED
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitIsState(t, proto.State_STOPPED, unitOut.Id, observed.Units) && unitIsState(t, proto.State_HEALTHY, unitIn.Id, observed.Units) {
				t.Logf("output has stopped, input is still healthy, restarting")
				restartedOutput = true
				unitOut.ConfigStateIdx++
				unitOut.State = proto.State_HEALTHY
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitIn,
						unitOut,
					},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && restartedOutput {
				t.Logf("output has restarted, is healthy")
				outputHealthy = true
				testStopped = true
				doneWaiter.Done()
			} else {
				for _, unit := range observed.Units {
					t.Logf("current state for %s is: %s", unit.Id, unit.Message)
				}
			}
		}
		return nil
	}

	runServerTest(t, srvFunc, doneWaiter, token)
	assert.True(t, gotConfig, "initial config")
	assert.True(t, stoppingOutput, "initial input stopped")
	assert.True(t, restartedOutput, "gRPC input has stopped")
	assert.True(t, outputHealthy, "input restarted")
	assert.True(t, testStopped, "test has shut down")
}

func TestStopAllInputs(t *testing.T) {
	token := mock.NewID()
	var gotConfig, stopped, grpcStopped, newInput, testStopped, inputRemoved bool

	var mut sync.Mutex
	_ = logp.DevelopmentSetup()

	unitOut, unitIn := basicStartingUnits(t)
	portAddr, _ := unitIn.Config.Source.AsMap()["server"].(string)

	doneWaiter := &sync.WaitGroup{}
	srvFunc := func(observed *proto.CheckinObserved) *proto.CheckinExpected {
		mut.Lock()
		defer mut.Unlock()
		if observed.Token == token {
			if unitsAreFailed(observed.Units) {
				t.Logf("Got failed unit")
				t.FailNow()
			}
			if testStopped {
				return nil
			}
			// initial checkin
			if len(observed.Units) == 0 || observed.Units[0].State == proto.State_STARTING {
				t.Logf("starting initial checkin")
				gotConfig = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && !stopped {
				t.Logf("Got unit state healthy, stopping input")
				stopped = true

				unitIn.ConfigStateIdx++
				unitIn.State = proto.State_STOPPED
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitIsState(t, proto.State_STOPPED, unitIn.Id, observed.Units) && stopped && !grpcStopped {
				t.Logf("input unit has stopped, removing")
				inputRemoved = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
					},
				}
			} else if !unitIsState(t, proto.State_HEALTHY, unitIn.Id, observed.Units) && stopped && inputRemoved && !grpcStopped {
				t.Logf("output has been marked as stopped; checking to see if gRPC has stopped")
				time.Sleep(time.Millisecond * 300)
				con, err := net.Dial("unix", portAddr)
				if err != nil {
					grpcStopped = true
				} else {
					_ = con.Close()
					doneWaiter.Done()
					testStopped = true
					t.Fatalf("socket is still open after outputs have closed")
				}

			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && grpcStopped && !newInput {
				t.Logf("input has stopped, starting again")
				newInput = true
				unitIn.ConfigStateIdx++
				unitIn.State = proto.State_HEALTHY
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && newInput {
				t.Logf("Units have restarted, stopping")
				doneWaiter.Done()
				testStopped = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{},
				}
			} else {
				for _, unit := range observed.Units {
					t.Logf("current state for %s is: %s", unit.Id, unit.Message)
				}
			}
		}
		return nil
	}

	runServerTest(t, srvFunc, doneWaiter, token)
	assert.True(t, gotConfig, "initial config")
	assert.True(t, stopped, "initial input stopped")
	assert.True(t, grpcStopped, "gRPC input has stopped")
	assert.True(t, newInput, "input restarted")
	assert.True(t, testStopped, "test has shut down")
	assert.True(t, inputRemoved, "input unit removed")
}

func TestChangeOutputType(t *testing.T) {
	token := mock.NewID()
	var gotConfig, updated, newOutput, gotStopped, outGotConfiguring bool

	var mut sync.Mutex
	_ = logp.DevelopmentSetup()

	unitOut, unitIn := basicStartingUnits(t)
	secondOutput := &proto.UnitExpectedConfig{
		Source: MustNewStruct(t, map[string]interface{}{
			"type":    "elasticsearch",
			"enabled": "true",
			"hosts":   []interface{}{"localhost:9200"},
		}),
	}

	doneWaiter := &sync.WaitGroup{}
	srvFunc := func(observed *proto.CheckinObserved) *proto.CheckinExpected {
		mut.Lock()
		defer mut.Unlock()
		if observed.Token == token {
			if unitsAreFailed(observed.Units) {
				t.Logf("Got failed unit")
				t.FailNow()
			}
			if gotStopped {
				return nil
			}
			// initial checkin
			if len(observed.Units) == 0 || observed.Units[0].State == proto.State_STARTING {
				t.Logf("starting initial checkin")
				gotConfig = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && !updated {
				t.Logf("Got unit state healthy, updating output")
				updated = true

				unitOut.ConfigStateIdx++
				unitOut.Config = secondOutput
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitIsState(t, proto.State_CONFIGURING, unitOut.Id, observed.Units) && updated {
				outGotConfiguring = true
				t.Logf("output is reconfiguring")

			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && updated && outGotConfiguring {
				t.Logf("units healthy after restart, stopping")
				newOutput = true

				unitOut.ConfigStateIdx++
				unitOut.State = proto.State_STOPPED
				unitIn.ConfigStateIdx++
				unitIn.State = proto.State_STOPPED
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitsAreState(t, proto.State_STOPPED, observed.Units) {
				gotStopped = true
				doneWaiter.Done()
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
		return nil
	}

	runServerTest(t, srvFunc, doneWaiter, token)
	assert.True(t, gotConfig, "initial config")
	assert.True(t, updated, "unit was updated")
	assert.True(t, newOutput, "unit got new output")
	assert.True(t, outGotConfiguring, "output reconfigured")
	assert.True(t, gotStopped, "units stopped")
}

func TestAddingInputs(t *testing.T) {
	token := mock.NewID()
	var gotConfig, addedSecond, stoppedFirst, gotStopped, gotRestarted bool

	var mut sync.Mutex
	_ = logp.DevelopmentSetup()

	unitOut, unitIn := basicStartingUnits(t)
	secondInUnit := createUnitExpectedState(fmt.Sprintf("%s-input2", mock.NewID()), proto.UnitType_INPUT, &proto.UnitExpectedConfig{
		Source: MustNewStruct(t, map[string]interface{}{
			"logging": map[string]interface{}{
				"level": "debug",
			},
			"server": fmt.Sprintf("/tmp/%s.sock", mock.NewID()),
		},
		),
	})

	doneWaiter := &sync.WaitGroup{}
	t.Logf("Creating mock server")
	srvFunc := func(observed *proto.CheckinObserved) *proto.CheckinExpected {
		mut.Lock()
		defer mut.Unlock()
		if observed.Token == token {
			if unitsAreFailed(observed.Units) {
				t.Logf("Got failed unit")
				t.FailNow()
			}
			if gotStopped {
				return nil
			}
			// initial checkin
			if len(observed.Units) == 0 || observed.Units[0].State == proto.State_STARTING {
				t.Logf("starting initial checkin")
				gotConfig = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && !addedSecond {
				t.Logf("Got unit state healthy, adding second input")
				addedSecond = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
						secondInUnit,
					},
				}
			} else if unitsAreState(t, proto.State_CONFIGURING, observed.Units) && (addedSecond || stoppedFirst) {
				// Adding a second or removing an additional unit shouldn't result in any components restarting
				gotRestarted = true
				gotStopped = true
				doneWaiter.Done()
				t.Logf("unit restarted after adding second input")
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && addedSecond && !stoppedFirst {
				// Stop the first unit. This should not effect the input/output
				t.Logf("removing first input unit")
				stoppedFirst = true
				unitIn.State = proto.State_STOPPED
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
						secondInUnit,
					},
				}
			} else if addedSecond && stoppedFirst && !unitsAreState(t, proto.State_STOPPED, observed.Units) {
				// remove all units
				t.Logf("stopping all units")
				secondInUnit.State = proto.State_STOPPED
				unitOut.State = proto.State_STOPPED
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
						secondInUnit,
					},
				}
			} else if unitsAreState(t, proto.State_STOPPED, observed.Units) {
				t.Logf("Got stopped, removing")
				gotStopped = true
				doneWaiter.Done()
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{},
				}
			} else {
				for _, unit := range observed.Units {
					t.Logf("current state for %s is: %s", unit.Id, unit.Message)
				}
			}
		}
		return nil
	}

	runServerTest(t, srvFunc, doneWaiter, token)

	assert.True(t, gotConfig, "config state")
	assert.True(t, gotStopped, "stopped state")
	assert.True(t, addedSecond, "added second unit")
	assert.True(t, stoppedFirst, "stopped first unit")
	assert.True(t, gotStopped, "units stopped")
	assert.False(t, gotRestarted, "units restarted, they should not")
}

func TestBasicAgentControl(t *testing.T) {
	token := mock.NewID()
	var gotConfig, gotHealthy, gotStopped bool

	var mut sync.Mutex
	_ = logp.DevelopmentSetup()

	unitOut, unitIn := basicStartingUnits(t)

	doneWaiter := &sync.WaitGroup{}
	t.Logf("Creating mock server")

	srvFunc := func(observed *proto.CheckinObserved) *proto.CheckinExpected {
		mut.Lock()
		defer mut.Unlock()
		if observed.Token == token {
			if unitsAreFailed(observed.Units) {
				t.Logf("Got failed unit")
				t.FailNow()
			}
			if gotStopped {
				return nil
			}
			// initial checkin
			if len(observed.Units) == 0 || observed.Units[0].State == proto.State_STARTING {
				t.Logf("starting initial checkin")
				gotConfig = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) {
				t.Logf("Got unit state healthy, sending STOPPED")
				gotHealthy = true
				// shutdown
				unitIn.ConfigStateIdx++
				unitIn.State = proto.State_STOPPED

				unitOut.ConfigStateIdx++
				unitOut.State = proto.State_STOPPED
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitsAreState(t, proto.State_STOPPED, observed.Units) {
				gotStopped = true
				doneWaiter.Done()
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
		return nil
	}

	runServerTest(t, srvFunc, doneWaiter, token)

	assert.True(t, gotConfig, "config state")
	assert.True(t, gotHealthy, "healthy state")
	assert.True(t, gotStopped, "stopped state")
}

func TestUnitLogChange(t *testing.T) {
	token := mock.NewID()
	var gotConfig, gotHealthy, gotStopped, gotRestarted bool

	var mut sync.Mutex
	_ = logp.DevelopmentSetup()

	unitOut, _ := basicStartingUnits(t)
	unitOut.LogLevel = proto.UnitLogLevel_INFO

	doneWaiter := &sync.WaitGroup{}
	t.Logf("Creating mock server")

	srvFunc := func(observed *proto.CheckinObserved) *proto.CheckinExpected {
		mut.Lock()
		defer mut.Unlock()
		if observed.Token == token {
			if unitsAreFailed(observed.Units) {
				t.Logf("Got failed unit")
				t.FailNow()
			}
			if gotStopped {
				return nil
			}
			// initial checkin
			if len(observed.Units) == 0 || observed.Units[0].State == proto.State_STARTING {
				t.Logf("starting initial checkin")
				// set log level to info
				logp.SetLevel(zapcore.InfoLevel)
				gotConfig = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
					},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && !gotHealthy {
				t.Logf("Got unit state healthy, changing log level")
				gotHealthy = true
				// shutdown
				unitOut.ConfigStateIdx++
				unitOut.LogLevel = proto.UnitLogLevel_DEBUG
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
					},
				}
			} else if unitsAreState(t, proto.State_CONFIGURING, observed.Units) && gotHealthy {
				// we shouldn't restart on log level update, so this is a fail
				gotRestarted = true
				gotStopped = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) && gotHealthy {
				// shut down
				t.Logf("Got unit state healthy, stopping")
				unitOut.ConfigStateIdx++
				unitOut.State = proto.State_STOPPED
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
					},
				}
			} else if unitsAreState(t, proto.State_STOPPED, observed.Units) {
				gotStopped = true
				doneWaiter.Done()
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
		return nil
	}

	runServerTest(t, srvFunc, doneWaiter, token)

	assert.True(t, gotConfig, "config state")
	assert.True(t, gotHealthy, "healthy state")
	assert.True(t, gotStopped, "stopped state")
	assert.False(t, gotRestarted, "units restarted")
}

func TestDiagnostics(t *testing.T) {
	_ = logp.DevelopmentSetup()
	var mut sync.Mutex
	token := mock.NewID()
	unitOut, unitIn := basicStartingUnits(t)
	var gotHealthy bool
	implFunc := func(observed *proto.CheckinObserved) *proto.CheckinExpected {
		mut.Lock()
		defer mut.Unlock()
		if observed.Token == token {
			if unitsAreFailed(observed.Units) {
				t.Logf("Got failed unit")
				t.FailNow()
			}
			// initial checkin
			if len(observed.Units) == 0 || observed.Units[0].State == proto.State_STARTING {
				t.Logf("starting initial checkin")
				// set log level to info
				//gotConfig = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			} else if unitsAreState(t, proto.State_HEALTHY, observed.Units) {
				t.Logf("Got unit state healthy, changing log level")
				gotHealthy = true
				return &proto.CheckinExpected{
					Units: []*proto.UnitExpected{
						unitOut,
						unitIn,
					},
				}
			}
		}
		return nil
	}

	srv := mock.StubServerV2{
		CheckinV2Impl: implFunc,
		ActionImpl: func(response *proto.ActionResponse) error {
			return nil
		},
		ActionsChan: make(chan *mock.PerformAction, 100),
		SentActions: make(map[string]*mock.PerformAction),
	}

	require.NoError(t, srv.Start())
	t.Logf("creating client")
	// connect with client
	validClient := createClient(token, srv.Port)

	t.Logf("starting shipper controller")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	go func() {
		err := runController(ctx, validClient)
		require.NoError(t, err)
	}()

	var diagRaw []byte
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("got timeout, shipper was not healthy")
			return
		default:
		}
		if gotHealthy {
			t.Logf("units are healthy, trying diagnostics with unit %s", unitOut.Id)
			resp, err := srv.PerformDiagnostic(unitOut.Id, proto.UnitType_OUTPUT)
			require.NoError(t, err)
			for _, diagItem := range resp {
				if diagItem.Name == "queue" {
					t.Logf("got queue data: %v", diagItem)
					diagRaw = diagItem.Content
				}
			}
			cancel()
			break
		}
		time.Sleep(time.Millisecond * 300)
	}

	if len(diagRaw) == 0 {
		t.Fatalf("queue diagnostics data was empty")
	}

	// quick check to make sure the data is actually there
	queueData := map[string]interface{}{}

	err := json.Unmarshal(diagRaw, &queueData)
	require.NoError(t, err)

	require.NotZero(t, queueData["max_level"])

}

func runServerTest(t *testing.T, implFunc mock.StubServerCheckinV2, waitUntil *sync.WaitGroup, token string) {
	_ = logp.DevelopmentSetup()

	srv := mock.StubServerV2{
		CheckinV2Impl: implFunc,
		ActionImpl: func(response *proto.ActionResponse) error {
			return nil
		},
		ActionsChan: make(chan *mock.PerformAction, 100),
	}

	require.NoError(t, srv.Start())
	waitUntil.Add(1)
	defer srv.Stop()

	t.Logf("creating client")
	// connect with client
	validClient := createClient(token, srv.Port)

	t.Logf("starting shipper controller")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	go func() {
		waitUntil.Wait()
		cancel()
	}()

	err := runController(ctx, validClient)
	assert.NoError(t, err)
}

func createClient(token string, port int) client.V2 {
	validClient := client.NewV2(fmt.Sprintf(":%d", port), token, client.VersionInfo{
		Name:    "program",
		Version: "v1.0.0",
		Meta: map[string]string{
			"key": "value",
		},
	}, grpc.WithTransportCredentials(insecure.NewCredentials()))
	return validClient
}

func basicStartingUnits(t *testing.T) (*proto.UnitExpected, *proto.UnitExpected) {
	unitOutputID := fmt.Sprintf("%s-output", mock.NewID())
	unitInputID := fmt.Sprintf("%s-input", mock.NewID())
	unitOut := createUnitExpectedState(unitOutputID, proto.UnitType_OUTPUT, &proto.UnitExpectedConfig{
		Source: MustNewStruct(t, map[string]interface{}{
			"logging": map[string]interface{}{
				"level": "debug",
			},
			"type":    "console",
			"enabled": "true",
		}),
	})
	unitIn := createUnitExpectedState(unitInputID, proto.UnitType_INPUT, &proto.UnitExpectedConfig{
		Source: MustNewStruct(t, map[string]interface{}{
			"logging": map[string]interface{}{
				"level": "debug",
			},
			"server": fmt.Sprintf("/tmp/%s.sock", mock.NewID()),
		},
		),
	})

	return unitOut, unitIn
}

func createUnitExpectedState(id string, unitType proto.UnitType, cfg *proto.UnitExpectedConfig) *proto.UnitExpected {
	return &proto.UnitExpected{
		Id:             id,
		Type:           unitType,
		ConfigStateIdx: 1,
		State:          proto.State_HEALTHY,
		LogLevel:       proto.UnitLogLevel_DEBUG,
		Config:         cfg,
	}
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

func unitIsState(t *testing.T, expected proto.State, id string, units []*proto.UnitObserved) bool {
	for _, unit := range units {
		if unit.Id == id && unit.State == expected {
			return true
		}
	}
	t.Logf("unit %s was not state %s", id, expected.String())
	return false
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
