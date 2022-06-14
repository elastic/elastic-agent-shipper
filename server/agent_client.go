package server

import (
	"context"
	"fmt"

	"github.com/elastic/beats/v7/libbeat/logp"
	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-client/v7/pkg/proto"
	"github.com/elastic/elastic-agent-shipper/config"
)

type doneChan chan struct{}

// clientInitFunc is a bit of a hack so we can init the client with a copy of the shipper client inside it
type clientInitFunc func(client.StateInterface) (client.Client, error)

// AgentClient implements the StateInterface used by the V1 elastic agent controller
type AgentClient struct {
	// stop Tells our main runloop to stop
	stop    doneChan
	client  client.Client
	shipper *shipperServer
	log     *logp.Logger
}

// NewShipperFromClient creates a new shipper client from an existing agent client
func NewShipperFromClient(clientInit clientInitFunc) (*AgentClient, error) {

	c := &AgentClient{
		stop:    make(doneChan),
		shipper: newShipper(),
		log:     logp.L(),
	}

	agentClient, err := clientInit(c)
	if err != nil {
		return nil, fmt.Errorf("error creating agent client: %w", err)
	}

	c.client = agentClient
	return c, nil
}

// OnConfig is called by the agent on a requested config change
func (client *AgentClient) OnConfig(in string) {
	client.log.Debugf("Got config update, (re)starting the shipper: %s", in)
	client.client.Status(proto.StateObserved_CONFIGURING, "configuring from OnConfig", nil)

	cfg, err := config.ReadConfigFromString(in)
	if err != nil {
		errString := fmt.Errorf("error reading string config: %w", err)
		client.client.Status(proto.StateObserved_FAILED, errString.Error(), nil)
	}

	go func() {
		// stop existing server
		client.shipper.Stop()
		//re-run
		err := client.shipper.Run(cfg)
		if err != nil {
			errString := fmt.Errorf("error starting shipper: %w", err)
			client.client.Status(proto.StateObserved_FAILED, errString.Error(), nil)
			return
		}
	}()

}

// OnStop is called by the agent to stop the shipper
func (client *AgentClient) OnStop() {
	client.log.Debugf("Stopping client")
	client.client.Status(proto.StateObserved_STOPPING, "Stopping shipper", nil)
	// do we need to worry about this being a blocking call? No idea.
	client.shipper.Stop()
	client.stop <- struct{}{}
}

// OnError is called by the agent to handle comm issues between client and server
func (client *AgentClient) OnError(err error) {
	client.log.Debugf("Error reported: %s", err)
}

func (client *AgentClient) StartClient(ctx context.Context) error {
	client.log.Debugf("Starting agent client")
	return client.client.Start(ctx)
}
