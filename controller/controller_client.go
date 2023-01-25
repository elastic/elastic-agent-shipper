// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"fmt"

	"github.com/mitchellh/mapstructure"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
)

type doneChan chan struct{}

// A little helper for managing the state of the main runloop
type clientHandler struct {
	// hashmap of units we get
	units *UnitMap

	shipperIsRunning bool
	runner           *ServerRunner
	log              *logp.Logger
}

func newClientHandler() clientHandler {
	return clientHandler{
		log:              logp.L(),
		units:            NewUnitMap(),
		shipperIsRunning: false,
	}
}

/*
//////////
////////// Shipper/output Handlers
/////////
*/

// ReloadShipperServer performs a (re)start of the shipper's backend components when triggered by another component
func (c *clientHandler) ReloadShipperServer() {
	// TODO: once https://github.com/elastic/elastic-agent-shipper/issues/225 is done, a lot of this logic will need to change
	if outUnit, gRPCConfig, ok := c.units.ShipperConfig(); ok { // do we have an output config?
		if state, _, _ := outUnit.Expected(); state == client.UnitStateHealthy { // does the agent want us to be running?
			// start shipper
			if !c.shipperIsRunning {
				c.log.Info("Starting shipper")
				c.startShipper(outUnit, gRPCConfig)
			} else { // shipper is already running, restart
				c.log.Info("Restarting shipper")
				c.stopShipper(outUnit)
				c.startShipper(outUnit, gRPCConfig)
			}
		} else if state == client.UnitStateStopped { // shut down
			c.log.Info("Stopping shipper")
			c.stopShipper(outUnit)
		} else {
			c.log.Errorf("Got output unit with unexpected state: %s", state.String())
		}
	} else {
		if c.shipperIsRunning { // we have missing config but the shipper is running, shut down.
			c.log.Info("Stopping shipper")
			c.stopShipper(outUnit)
		}
	}

}

// initialize the startup of the shipper grpc server, queues and other backend components of the output
func (c *clientHandler) startShipper(outUnit *client.Unit, grpcUnit ShipperUnit) {
	_ = outUnit.UpdateState(client.UnitStateConfiguring, "reading shipper config", nil)
	_, level, unitConfig := outUnit.Expected()

	cfg, err := config.ShipperConfigFromUnitConfig(level, unitConfig, grpcUnit.Conn)
	if err != nil {
		c.reportError("error configuring shipper", err, outUnit)
		return
	}

	c.log.Debugf("Starting Shipper with endpoint '%s'", cfg.Shipper.Server.Server)

	//TODO: until we get TLS config fixed/figured out, run in insecure mode
	// certPool := x509.NewCertPool()
	// for _, cert := range cfg.Shipper.Server.TLS.CAs {
	// 	if ok := certPool.AppendCertsFromPEM([]byte(cert)); !ok {
	// 		c.reportError("error appending cert obtained from input in shipper startup", err, outUnit)
	// 		return
	// 	}
	// }

	creds := insecure.NewCredentials() //:= credentials.NewTLS(&tls.Config{
	// 	ClientAuth:     tls.RequireAndVerifyClientCert,
	// 	ClientCAs:      certPool,
	// 	GetCertificate: c.getCertificate,
	// 	MinVersion:     tls.VersionTLS12,
	// })

	runner, err := NewOutputServer(cfg, creds, outUnit, grpcUnit.Unit)
	if err != nil {
		c.log.Debugf("failed to create a new output server: %s", err)
		return
	}
	c.runner = runner

	outUnit.RegisterDiagnosticHook("queue", "queue metrics", "", "application/json", runner.monitoring.DiagnosticsCallback())

	_ = outUnit.UpdateState(client.UnitStateHealthy, "Shipper Running", nil)

	go func() {
		c.shipperIsRunning = true
		err := c.runner.Start()
		if err != nil {
			c.reportError("error running shipper server", err, outUnit)
			c.shipperIsRunning = false
		}
	}()

}

// close the runner connection. Called from BounceShipper
func (c *clientHandler) stopShipper(outUnit *client.Unit) {
	if c.shipperIsRunning {
		err := c.runner.Close()
		if err != nil {
			c.reportError("error running shipper server", err, outUnit)
		}
		c.shipperIsRunning = false
	}
}

/*
//////////
////////// Input Handlers
/////////
*/

// start an individual input stream
func (c *clientHandler) inputAdded(unit *client.Unit) {
	// I assume that once we connect output processors, that will go somewhere here
	_, _, cfg := unit.Expected()
	// decode the gRPC config used by the shipper
	conn := config.ShipperClientConfig{}
	err := mapstructure.Decode(cfg.Source.AsMap(), &conn)
	if err != nil {
		c.reportError("error unpacking input config", err, unit)
		return
	}
	c.units.AddUnit(unit, conn)
	c.log.Debugf("Got client %s with config: Server: %s", unit.ID(), conn.Server)
	_ = unit.UpdateState(client.UnitStateHealthy, "healthy", nil)

}

/*
//////////
////////// Generic event Handlers, called from the V2 listener runloop
/////////
*/

// handle the UnitChangedAdded event from the V2 API
// returns true if the shipper component needs to be bounced.
func (c *clientHandler) handleUnitAdded(unit *client.Unit) bool {
	unitType := unit.Type()
	state, logLvl, _ := unit.Expected()
	c.log.Debugf("Got unit added for ID %s (%s/%s)", unit.ID(), state.String(), logLvl.String())
	needsRestart := false
	if unitType == client.UnitTypeOutput {
		c.units.SetOutput(unit)
		if c.units.ShipperHasConfig() { // case: we got the input unit first, now we have the output, and can start
			needsRestart = true
		}
	}
	if unitType == client.UnitTypeInput { // unit startup for inputs
		c.inputAdded(unit)
		if c.units.ShipperHasConfig() && !c.shipperIsRunning { // case: we got the output first, now we have an input, and can start
			needsRestart = true
		}
	}

	return needsRestart
}

// handle the UnitChangedModified event from the V2 API
// returns true if the shipper component needs to be bounced.
func (c *clientHandler) handleUnitUpdated(unit *client.Unit) bool {
	state, logLvl, _ := unit.Expected()
	c.log.Debugf("Got unit updated for ID %s (%s/%s)", unit.ID(), state.String(), logLvl.String())
	needsRestart := false
	unitType := unit.Type()
	if unitType == client.UnitTypeOutput {
		c.units.SetOutput(unit)
		needsRestart = true
	} else {
		c.units.UpdateUnit(unit)
	}
	return needsRestart
}

// handle the UnitChangedRemoved event from the V2 API
// returns true if the shipper component needs to be bounced.
func (c *clientHandler) handleUnitRemoved(unit *client.Unit) bool {
	state, logLvl, _ := unit.Expected()
	c.log.Debugf("Got unit removed for ID %s (%s/%s)", unit.ID(), state.String(), logLvl.String())
	unitType := unit.Type()
	if unitType == client.UnitTypeInput {
		c.units.DeleteInput(unit)
	} else {
		c.units.DeleteOutput()
	}

	// if the unit is no longer in a runnable state, the bounce will stop it
	return !c.units.ShipperHasConfig()
}

/*
//////////
////////// Helpers
/////////
*/

// used for the tls GetCertificate function in TLS
// Ideally, will be removed

// func (c *clientHandler) getCertificate(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
// 	unit, found := c.units.GetUnitByServer(chi.ServerName)
// 	if !found {
// 		return nil, fmt.Errorf("No TLS connection info found for server %s", chi.ServerName)
// 	}

// 	if unit.Conn.TLS.Cert == "" || unit.Conn.TLS.Key == "" {
// 		return nil, fmt.Errorf("no TLS config for server %s", chi.ServerName)
// 	}

// 	cert, err := tls.X509KeyPair([]byte(unit.Conn.TLS.Cert), []byte(unit.Conn.TLS.Key))
// 	if err != nil {
// 		return nil, fmt.Errorf("error creating keypair for input: %s", err)
// 	}
// 	c.log.Debugf("%s has cert %#v", chi.ServerName, unit.Conn.TLS)
// 	return &cert, nil
// }

// helper for reporting errors across the logger and the state reporter
func (c *clientHandler) reportError(errorMsg string, err error, unit *client.Unit) {
	wrapped := fmt.Sprintf("%s: %s", errorMsg, err)
	c.log.Errorf(wrapped)
	_ = unit.UpdateState(client.UnitStateFailed, wrapped, nil)
}
