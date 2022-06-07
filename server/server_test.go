package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-client/v7/pkg/client/mock"
	"github.com/elastic/elastic-agent-client/v7/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestAgentControl(t *testing.T) {
	unitOneID := mock.NewID()

	token := mock.NewID()
	//gotValid := false
	//gotInvalid := false
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
					return &proto.CheckinExpected{
						Units: []*proto.UnitExpected{
							{
								Id:             unitOneID,
								Type:           proto.UnitType_OUTPUT,
								ConfigStateIdx: 1,
								Config:         `{"logging": {"level": "debug"}}`, // hack to make my life easier
								State:          proto.State_HEALTHY,
							},
						},
					}
				} else if observed.Units[0].State == proto.State_HEALTHY {
					//shutdown
					return &proto.CheckinExpected{
						Units: []*proto.UnitExpected{
							{
								Id:             unitOneID,
								Type:           proto.UnitType_OUTPUT,
								ConfigStateIdx: 1,
								Config:         "{}",
								State:          proto.State_STOPPED,
							},
						},
					}
				} else if observed.Units[0].State == proto.State_STOPPED {
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
}

func unitsAreHealthy(observed []*proto.UnitObserved) bool {
	if len(observed) < 1 {
		return false
	}
	for _, unit := range observed {
		state := unit.State
		if state != proto.State_HEALTHY {
			return false
		}
	}
	return true
}
