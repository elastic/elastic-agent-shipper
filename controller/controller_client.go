// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"fmt"

	"google.golang.org/grpc/credentials/insecure"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"

	libcfg "github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
	"github.com/elastic/elastic-agent-shipper/grpcserver"
	"github.com/elastic/elastic-agent-shipper/publisherserver"
)

// A little helper for managing the state of the main runloop
type clientHandler struct {
	// hashmap of units we get
	units *UnitMap
	log   *logp.Logger

	// handlers for input and output components
	grpcServer    *grpcserver.InputHandler
	outputHandler *publisherserver.ServerRunner
}

func newClientHandler() clientHandler {
	handler := clientHandler{
		log:           logp.L(),
		units:         NewUnitMap(),
		outputHandler: publisherserver.NewOutputServer(),
	}
	handler.grpcServer = grpcserver.NewGRPCServer(handler.outputHandler)
	return handler
}

/*
//////////
////////// Shipper/output Handlers
/////////
*/

// close the runner connection. Called from BounceShipper
func (c *clientHandler) stopShipper() {
	err := c.outputHandler.Close()
	if err != nil {
		c.log.Errorf("error stopping shipper: %s", err)
	}
}

func (c *clientHandler) startShipper(unit *client.Unit) {
	_ = unit.UpdateState(client.UnitStateConfiguring, "reading shipper config", nil)
	_, level, unitConfig := unit.Expected()

	cfg, err := config.ShipperConfigFromUnitConfig(level, unitConfig)
	if err != nil {
		c.reportError("error configuring shipper", err, unit)
		if c.grpcServer != nil {
			c.grpcServer.InitHasFailed("error configuring shipper")
		}
		return
	}

	err = c.outputHandler.Start(cfg)
	if err != nil {
		c.reportError("error starting output server shipper", err, unit)
		if c.grpcServer != nil {
			c.grpcServer.InitHasFailed("error starting output server shipper")
		}
		return
	}
	_ = unit.UpdateState(client.UnitStateHealthy, "outputs initialized", nil)
}

// called when we get a UnitUpdated for the output unit
func (c *clientHandler) updateShipperOutput(unit *client.Unit) {
	if unit.Type() != client.UnitTypeOutput {
		c.log.Errorf("updateShipperOutput got a non-output unit of ID %s", unit.ID())
		return
	}
	c.log.Debugf("updating output unit %s", unit.ID())
	state, _, _ := unit.Expected()

	c.units.UpdateUnit(unit)
	if state == client.UnitStateHealthy { // config update, so restart
		_ = unit.UpdateState(client.UnitStateStopping, "shipper is restarting", nil)
		c.stopShipper()
		c.startShipper(unit)
	} else if state == client.UnitStateStopped { // shut down
		_ = unit.UpdateState(client.UnitStateStopping, "shipper is stopping", nil)
		c.stopShipper()
		_ = unit.UpdateState(client.UnitStateStopped, "shipper is stopped", nil)
	}
}

/*
//////////
////////// Input Handlers
/////////
*/

func (c *clientHandler) startgRPC(unit *client.Unit, cfg config.ShipperConnectionConfig) {
	//TODO: until we get TLS config fixed/figured out, run in insecure mode
	// certPool := x509.NewCertPool()
	// for _, cert := range cfg.Shipper.Server.TLS.CAs {
	// 	if ok := certPool.AppendCertsFromPEM([]byte(cert)); !ok {
	// 		c.reportError("error appending cert obtained from input in shipper startup", err, outUnit)
	// 		return
	// 	}
	// }

	_ = unit.UpdateState(client.UnitStateConfiguring, "starting gRPC server", nil)
	creds := insecure.NewCredentials() //:= credentials.NewTLS(&tls.Config{
	// 	ClientAuth:     tls.RequireAndVerifyClientCert,
	// 	ClientCAs:      certPool,
	// 	GetCertificate: c.getCertificate,
	// 	MinVersion:     tls.VersionTLS12,
	// })

	err := c.grpcServer.Start(creds, cfg.Server)
	if err != nil {
		c.reportError("failed to start grpc server", err, unit)
		return
	}
	c.log.Debugf("gRPC started")
	_ = unit.UpdateState(client.UnitStateHealthy, "gRPC started", nil)
}

func (c *clientHandler) stopGRPC() {
	c.grpcServer.Stop()
	c.log.Debugf("gRPC server stopped")
}

// start an individual input stream
func (c *clientHandler) addInput(unit *client.Unit) {
	_, _, cfg := unit.Expected()
	// decode the gRPC config used by the shipper
	conn := config.ShipperConnectionConfig{}
	cfgObj, err := libcfg.NewConfigFrom(cfg.Source.AsMap())
	if err != nil {
		c.reportError("error creating config object", err, unit)
		return
	}
	err = cfgObj.Unpack(&conn)
	if err != nil {
		c.reportError("error unpacking input config", err, unit)
		return
	}

	c.log.Debugf("Got client %s with config: Server: %s", unit.ID(), conn.Server)
	_ = unit.UpdateState(client.UnitStateHealthy, "healthy", nil)

	// figure out if we need to initialize the gRPC endpoint
	// TODO: this is another thing that will change with https://github.com/elastic/elastic-agent-shipper/issues/225,
	// As we'll have a dedicated unit for the input, and we won't be using random input unit updates to see if we need to update the gRPC endpoint.
	c.startgRPC(unit, conn)

}

// called when we get a UnitUpdated for an input
func (c *clientHandler) updateInput(unit *client.Unit) {
	c.log.Debugf("updating input unit %s", unit.ID())
	state, _, _ := unit.Expected()

	// For now, assume the gRPC config is static, don't update the server when we get input updates
	c.units.UpdateUnit(unit)
	if state == client.UnitStateStopped {
		_ = unit.UpdateState(client.UnitStateStopped, "unit has stopped", nil)
	}

}

/*
//////////
////////// Generic event Handlers, called from the V2 listener runloop
/////////
*/

// handle the UnitChangedAdded event from the V2 API
func (c *clientHandler) handleUnitAdded(unit *client.Unit) {
	unitType := unit.Type()
	state, logLvl, _ := unit.Expected()
	c.log.Infof("Got unit added for ID %s (%s/%s)", unit.ID(), state.String(), logLvl.String())
	c.units.AddUnit(unit)
	if unitType == client.UnitTypeOutput {
		c.startShipper(unit)
	}
	if unitType == client.UnitTypeInput { // unit startup for inputs
		c.addInput(unit)
	}
}

// handle the UnitChangedModified event from the V2 API
func (c *clientHandler) handleUnitUpdated(unit *client.Unit) {
	state, logLvl, cfg := unit.Expected()
	c.log.Infof("Got unit updated for ID %s (%s/%s)", unit.ID(), state.String(), logLvl.String())
	currentUnit := c.units.GetUnit(unit.ID(), unit.Type())
	// check to see if only the log level needs updating. Only update if we have an output unit, since we're basically getting copies of input units,
	// so a log level change for an input might not be for us.
	if onlyLogLevelUpdated(cfg.Source.AsMap(), currentUnit.config, logLvl) && unit.Type() == client.UnitTypeOutput {
		c.log.Infof("unit %s got update with only log level changing. Updating to %s", unit.ID(), config.ZapFromUnitLogLevel(logLvl))
		logp.SetLevel(config.ZapFromUnitLogLevel(logLvl))
		_ = unit.UpdateState(client.UnitStateHealthy, "log level changed", nil)
		return
	}

	unitType := unit.Type()
	if unitType == client.UnitTypeOutput {
		c.updateShipperOutput(unit)
	} else {
		c.updateInput(unit)
	}
}

// handle the UnitChangedRemoved event from the V2 API
func (c *clientHandler) handleUnitRemoved(unit *client.Unit) {
	state, logLvl, _ := unit.Expected()
	c.log.Debugf("Got unit removed for ID %s (%s/%s)", unit.ID(), state.String(), logLvl.String())
	c.units.DeleteUnit(unit)
	// until we have a dedicated unit for gRPC, use this as a sign to shut down the input
	if c.units.AvailableUnitCount() == 0 {
		c.log.Debugf("All input units are removed, stopping gRPC")
		c.stopGRPC()
		_ = unit.UpdateState(client.UnitStateStopped, "gRPC server stopped", nil)

	}
	_ = unit.UpdateState(client.UnitStateStopped, "unit removed", nil)

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
