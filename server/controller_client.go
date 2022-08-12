// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package server

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
)

type doneChan chan struct{}

// A little helper for managing the state of the main runloop
type clientHandler struct {
	// tells the shipper server to begin shutdown
	shutdownInit doneChan
	// Tells the controller that the shipper backend has gracefully shut down.
	shutdownComplete sync.WaitGroup

	unitsMut sync.Mutex
	units    map[string]*client.Unit

	// the unit ID for the shipper itself
	shipperOutputID string

	shipperIsStopping uint32

	log *logp.Logger
}

func newClientHandler() clientHandler {
	return clientHandler{
		shutdownInit:      make(doneChan, 1),
		log:               logp.L(),
		units:             make(map[string]*client.Unit),
		shipperIsStopping: 0,
	}
}

func (c *clientHandler) addUnit(unit *client.Unit) {
	c.unitsMut.Lock()
	c.units[unit.ID()] = unit
	c.unitsMut.Unlock()
}

func (c *clientHandler) getUnit(ID string) *client.Unit {
	c.unitsMut.Lock()
	defer c.unitsMut.Unlock()
	return c.units[ID]

}

func (c *clientHandler) deleteUnit(unit *client.Unit) {
	c.unitsMut.Lock()
	delete(c.units, unit.ID())
	c.unitsMut.Unlock()
}

// We'll need to track the client ID that's used for the shipper backend itself.
func (c *clientHandler) setShipperUnitID(unit *client.Unit) {
	c.shipperOutputID = unit.ID()
}

// async stop of the shipper's GRPC server, and anything else it needs to manually shutdown
func (c *clientHandler) stopShipper() {
	c.log.Debugf("Stopping Shipper")

	c.shutdownInit <- struct{}{}
}

// initialize the startup of the shipper grpc server and backend
func (c *clientHandler) startShipper(unit *client.Unit) {
	c.log.Debugf("Starting Shipper")
	atomic.CompareAndSwapUint32(&c.shipperIsStopping, 1, 0)

	// deciding to omit some of these error checks, as the client update state will only return an error if it has a JSON payload to unmarshall
	_ = unit.UpdateState(client.UnitStateConfiguring, "reading shipper config", nil)

	// Assuming that if we got here from UnitChangedAdded, we don't need to care about the expected state?
	_, level, unitConfig := unit.Expected()
	cfg, err := config.ShipperConfigFromUnitConfig(level, unitConfig)
	if err != nil {
		c.log.Errorf("error unpacking config from agent: %s", err)
		_ = unit.UpdateState(client.UnitStateFailed, err.Error(), nil)
		return
	}

	err = logp.Configure(cfg.Log)
	if err != nil {
		c.log.Errorf("error unpacking config from agent: %s", err)
		_ = unit.UpdateState(client.UnitStateFailed, err.Error(), nil)
		return
	}

	// reset the local logger
	c.log = logp.L()

	err = c.Run(cfg, unit)
	if err != nil {
		c.log.Errorf("error running shipper: %s", err)
		_ = unit.UpdateState(client.UnitStateFailed, err.Error(), nil)
		return
	}
}

// start an individual input stream
func (c *clientHandler) startStream(unit *client.Unit) {
	// when we have individual input streams, that'll go here.
	c.log.Debugf("Got unit stream for processor: %s", unit.ID())
	_ = unit.UpdateState(client.UnitStateHealthy, "healthy", nil)

}

// update an individual input stream
func (c *clientHandler) updateStream(unit *client.Unit) {
	// when we have individual input streams, that'll go here.
	c.log.Debugf("Got unit input update for processor: %s", unit.ID())
	_ = unit.UpdateState(client.UnitStateConfiguring, "updating", nil)
	_ = unit.UpdateState(client.UnitStateHealthy, "healthy", nil)

}

// handle the UnitChangedAdded event from the V2 API
func (c *clientHandler) handleUnitAdded(unit *client.Unit) {
	c.addUnit(unit)
	unitType := unit.Type()

	if unitType == client.UnitTypeOutput { // unit startup for the shipper itself
		c.setShipperUnitID(unit)
		go c.startShipper(unit)
	}
	if unitType == client.UnitTypeInput { // unit startup for streams? Processors? Queue?
		go c.startStream(unit)
	}
}

// handle the UnitChangedModified event from the V2 API
func (c *clientHandler) handleUnitUpdated(unit *client.Unit) {
	c.log.Debugf("Got Unit Modified: %s", unit.ID())
	unitType := unit.Type()

	if unitType == client.UnitTypeOutput {
		state, _, _ := unit.Expected()
		if state == client.UnitStateStopped {
			c.shutdown(unit)
		}
	} else {
		c.updateStream(unit)
	}
}

// a blocking call that will wait for the shipper components to gracefully shutdown, then send the unit update
func (c *clientHandler) shutdown(shipperUnit *client.Unit) {
	swapped := atomic.CompareAndSwapUint32(&c.shipperIsStopping, 0, 1)
	if !swapped {
		return
	}
	_ = shipperUnit.UpdateState(client.UnitStateStopping, "shutting down shipper output", nil)
	c.stopShipper()
	// The server has successfully shut down
	// In theory we want this to block, as we should wait for queues & outputs to empty.
	c.shutdownComplete.Wait()
	c.log.Debugf("Shutdown complete, sending STOPPPED")
	_ = shipperUnit.UpdateState(client.UnitStateStopped, "Stopped shipper output", nil)
}

// runController is the main runloop for the shipper itself, and managed communication with the agent.
func runController(ctx context.Context, agentClient client.V2) error {
	log := logp.L()

	err := agentClient.Start(ctx)
	if err != nil {
		return fmt.Errorf("error starting connection to client")
	}

	log.Debugf("Starting error reporter")
	go reportErrors(ctx, agentClient)

	log.Debugf("Started client, waiting")

	handler := newClientHandler()

	// receive the units
	for {
		select {
		case <-ctx.Done():
			handler.log.Debugf("Got context done")
			shipperUnit := handler.getUnit(handler.shipperOutputID)
			handler.shutdown(shipperUnit)
			// If we get context done, just hard-stop
			return nil

		case change := <-agentClient.UnitChanges():

			switch change.Type {
			case client.UnitChangedAdded: // The agent is starting the shipper, or we added a new processor
				go handler.handleUnitAdded(change.Unit)
			case client.UnitChangedModified: // config for a unit has changed
				go handler.handleUnitUpdated(change.Unit)
			case client.UnitChangedRemoved: // a unit has been stopoped and can now be removed.
				handler.deleteUnit(change.Unit)
				// for now, consider a remove of the shipper unit to be our sign to shut down.
				// Take care, as we won't get this until we've sent a STOPPED to the agent
				// TODO: we should have a timeout so we can shutdown without getting a REMOVED event
				if change.Unit.Type() == client.UnitTypeOutput {
					handler.log.Debugf("shipper unit removed, ending.")
					return nil
				}

			}
		}
	}
}

// I am not net sure how this should work or what it should do, but we need to read from that error channel
func reportErrors(ctx context.Context, agentClient client.V2) {
	log := logp.L()
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-agentClient.Errors():
			log.Errorf("Got error from controller: %s", err)
		}
	}
}
