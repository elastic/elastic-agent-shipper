// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/mitchellh/mapstructure"

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

	units *UnitMap

	shipperIsStopping uint32
	shipperIsRunning  uint32

	log *logp.Logger
}

func newClientHandler() clientHandler {
	return clientHandler{
		shutdownInit:      make(doneChan, 1),
		log:               logp.L(),
		units:             NewUnitMap(),
		shipperIsStopping: 0,
	}
}

/*
//////////
////////// Shipper/output Handlers
/////////
*/

// Run starts the gRPC server
func (c *clientHandler) Run(cfg config.ShipperRootConfig, unit *client.Unit) (err error) {
	_ = unit.UpdateState(client.UnitStateConfiguring, "Initialising shipper server", nil)
	runner, err := NewServerRunner(cfg)
	if err != nil {
		return err
	}

	handleShutdown(func() { _ = runner.Close() }, c.shutdownInit)

	// This will get sent after the server has shutdown, signaling to the runloop that it can stop.
	// The shipper has no queues connected right now, but once it does, this function can't run until
	// after the queues have emptied and/or shutdown. We'll presumably have a better idea of how this
	// will work once we have queues connected here.
	defer func() {
		c.log.Debugf("shipper has completed shutdown, stopping")
		c.shutdownComplete.Done()
	}()
	c.shutdownComplete.Add(1)

	_ = unit.UpdateState(client.UnitStateHealthy, "Shipper Running", nil)
	return runner.Start()
}

// async stop of the shipper's GRPC server, and anything else it needs to manually shutdown
func (c *clientHandler) stopShipper() {
	c.log.Debugf("Stopping Shipper")

	c.shutdownInit <- struct{}{}
}

// initialize the startup of the shipper grpc server and backend
func (c *clientHandler) startShipper(unit *client.Unit) {
	if c.shipperIsRunning == 1 {
		c.log.Warnf("WARNING: shipper is already running, but recieved another shipper output config from Unit %s", unit.ID())
		return
	}

	// deciding to omit some of these error checks, as the client update state will only return an error if it has a JSON payload to unmarshall
	_ = unit.UpdateState(client.UnitStateConfiguring, "reading shipper config", nil)

	// Assuming that if we got here from UnitChangedAdded, we don't need to care about the expected state?
	_, level, unitConfig := unit.Expected()

	cfg, err := config.ShipperConfigFromUnitConfig(level, unitConfig, unit.ID())
	if err != nil {
		c.reportError("error configuring shipper", err, unit)
		return
	}
	if !cfg.Shipper.Enabled {
		c.log.Warnf("Got output config but shipper was set to disabled: %s", unit.ID())
		return
	}
	c.units.SetOutput(unit)
	c.log.Debugf("Starting Shipper for ID '%s'", unit.ID())
	atomic.CompareAndSwapUint32(&c.shipperIsStopping, 1, 0)

	err = c.Run(cfg, unit)
	if err != nil {
		c.reportError("error running shipper", err, unit)
		return
	}
	atomic.CompareAndSwapUint32(&c.shipperIsRunning, 0, 1)
}

/*
//////////
////////// Input Handlers
/////////
*/

// start an individual input stream
func (c *clientHandler) startInput(unit *client.Unit) {
	// when we have individual input streams, that'll go here.
	_, _, cfg := unit.Expected()
	conn := config.ShipperClientConfig{}
	err := mapstructure.Decode(cfg.Source.AsMap(), &conn)
	if err != nil {
		c.reportError("error unpacking input config", err, unit)
		return
	}
	c.units.AddUnit(unit, conn)

	c.log.Debugf("Got client with config: Server: %s, CAs: %d", conn.Server, len(conn.TLS.CAs))

	_ = unit.UpdateState(client.UnitStateHealthy, "healthy", nil)

}

// update an individual input stream
func (c *clientHandler) updateInput(unit *client.Unit) {
	// when we have individual input streams, that'll go here.
	c.log.Debugf("Got unit input update for processor: %s", unit.ID())
	_ = unit.UpdateState(client.UnitStateConfiguring, "updating", nil)
	_ = unit.UpdateState(client.UnitStateHealthy, "healthy", nil)
}

/*
//////////
////////// Generic event Handlers
/////////
*/

// handle the UnitChangedAdded event from the V2 API
func (c *clientHandler) handleUnitAdded(unit *client.Unit) {
	unitType := unit.Type()
	//updatedState, _, newCfg := unit.Expected()
	//logp.L().Debugf("got config ADDED for unit %s (expected: %s): %s", unit.ID(), updatedState.String(), mapstr.M(newCfg.Source.AsMap()).StringToPrint())
	if unitType == client.UnitTypeOutput { // unit startup for the shipper itself
		//c.setShipperUnitID(unit)
		go c.startShipper(unit)
	}
	if unitType == client.UnitTypeInput { // unit startup for inputs

		go c.startInput(unit)
	}
}

// handle the UnitChangedModified event from the V2 API
func (c *clientHandler) handleUnitUpdated(unit *client.Unit) {
	c.log.Debugf("Got Unit Modified: %s", unit.ID())
	unitType := unit.Type()

	//updatedState, _, newCfg := unit.Expected()
	//logp.L().Debugf("got config MODIFIED for unit %s (expected: %s): %s", unit.ID(), updatedState.String(), mapstr.M(newCfg.Source.AsMap()).StringToPrint())
	if unitType == client.UnitTypeOutput {
		state, _, _ := unit.Expected()
		if state == client.UnitStateStopped {
			c.shutdown(unit)
		}
	} else {
		c.updateInput(unit)
	}
}

/*
//////////
////////// Helpers
/////////
*/

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
	atomic.CompareAndSwapUint32(&c.shipperIsRunning, 1, 0)
}

func (c *clientHandler) reportError(errorMsg string, err error, unit *client.Unit) {
	wrapped := fmt.Sprintf("%s: %s", errorMsg, err)
	c.log.Errorf(wrapped)
	_ = unit.UpdateState(client.UnitStateFailed, wrapped, nil)
}
