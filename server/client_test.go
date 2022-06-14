package server

import (
	"context"
	"fmt"
	"io/ioutil"
	"sync"
	"testing"
	"time"

	"github.com/elastic/beats/v7/libbeat/logp"
	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-client/v7/pkg/client/mock"
	"github.com/elastic/elastic-agent-client/v7/pkg/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestAgentClient(t *testing.T) {
	var m sync.Mutex
	token := "expected_token"
	baseCfg, err := readMainConfig()
	require.NoError(t, err)
	srv := mock.StubServer{
		CheckinImpl: func(observed *proto.StateObserved) *proto.StateExpected {
			m.Lock()
			defer m.Unlock()
			t.Logf("At checkin")
			if observed.Token == token {
				if observed.ConfigStateIdx == 0 { // initial state
					t.Logf("Got ConfigStateIdx 0")
					//connected = true
					return &proto.StateExpected{
						State:          proto.StateExpected_RUNNING,
						ConfigStateIdx: 1,
						Config:         baseCfg,
					}
				} else if observed.ConfigStateIdx == 1 { // try another update
					t.Logf("Got ConfigStateIdx 1")
					// status = observed.Status
					// message = observed.Message
					// payload = observed.Payload
					// if status == proto.StateObserved_HEALTHY {
					// 	healthyCount++
					// }
					return &proto.StateExpected{ // shutdown?
						State:          proto.StateExpected_RUNNING,
						ConfigStateIdx: 2,
						Config:         baseCfg,
					}
				} else if observed.ConfigStateIdx == 2 {
					return &proto.StateExpected{ // shutdown?
						State:          proto.StateExpected_STOPPING,
						ConfigStateIdx: 2,
						Config:         "",
					}
				}
			}
			// disconnect
			return nil
		},
		ActionImpl: func(response *proto.ActionResponse) error {
			// actions not tested here
			return nil
		},
		ActionsChan: make(chan *mock.PerformAction, 100),
	}

	require.NoError(t, srv.Start())

	// setup wrapper

	testWrapper := func(agentClient client.StateInterface) (client.Client, error) {
		return client.New(fmt.Sprintf(":%d", srv.Port), token, agentClient, nil, grpc.WithTransportCredentials(insecure.NewCredentials())), nil
	}

	testCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	logp.DevelopmentSetup()
	shipper, err := NewShipperFromClient(testWrapper)
	require.NoError(t, err)
	// start the server runtime
	err = runAgentClient(testCtx, shipper)
	require.NoError(t, err)

}

// quick test hack to read in the main config so we send it to the shipper via agent control
func readMainConfig() (string, error) {
	cfgPath := "../elastic-agent-shipper.yml"
	fileBytes, err := ioutil.ReadFile(cfgPath)
	if err != nil {
		return "", fmt.Errorf("error reading in %s: %w", cfgPath, err)
	}
	return string(fileBytes), nil
}
