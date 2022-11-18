// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"sync"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-shipper/config"
)

// UnitMap is a wrapper for safely handling the map of V2 client units
type UnitMap struct {
	mut sync.Mutex
	// The map of input-type units
	// These will presumably be used during the startup phase of the shipper
	// communicating with an input, but not sure how yet
	inputUnits map[string]ShipperUnit
	outputUnit *client.Unit
}

// NewUnitMap creates a new Unit manager
func NewUnitMap() *UnitMap {
	return &UnitMap{
		inputUnits: make(map[string]ShipperUnit),
	}
}

// AddUnit adds a unit
func (c *UnitMap) AddUnit(unit *client.Unit, conn config.ShipperClientConfig) {
	c.mut.Lock()
	defer c.mut.Unlock()
	c.inputUnits[unit.ID()] = ShipperUnit{Unit: unit, Conn: conn}

}

// GetUnit returns a unit for a given ID
func (c *UnitMap) GetUnit(ID string) ShipperUnit {
	c.mut.Lock()
	defer c.mut.Unlock()
	return c.inputUnits[ID]
}

// DeleteUnit removes a unit, called after the Client sends a unit remove event
func (c *UnitMap) DeleteUnit(unit *client.Unit) {
	c.mut.Lock()
	defer c.mut.Unlock()
	delete(c.inputUnits, unit.ID())

}

// SetOutput sets the "main" output unit
func (c *UnitMap) SetOutput(unit *client.Unit) {
	c.mut.Lock()
	defer c.mut.Unlock()
	c.outputUnit = unit
}

// GetOutput returns the main shipper output unit
func (c *UnitMap) GetOutput() *client.Unit {
	c.mut.Lock()
	defer c.mut.Unlock()
	return c.outputUnit
}

// ShipperUnit wraps the available config for a unit
type ShipperUnit struct {
	Unit *client.Unit
	Conn config.ShipperClientConfig
}
