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
	inputUnits map[string]ShipperUnit
	outputUnit *client.Unit
}

// ShipperUnit wraps the available config for a unit
type ShipperUnit struct {
	Unit *client.Unit
	// The connection config associated with an input unit is a bit "special", as it's a static value
	// that's valid across the lifetime of the shipper. So, separate out that config from the rest of the unit config
	Conn config.ShipperClientConfig
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

// UpdateUnit updates the unit with a new state
func (c *UnitMap) UpdateUnit(unit *client.Unit) {
	c.mut.Lock()
	defer c.mut.Unlock()
	// Probably a bug in elastic-agent if we hit this
	current, ok := c.inputUnits[unit.ID()]
	if !ok {
		return
	}
	current.Unit = unit
	c.inputUnits[unit.ID()] = current
}

// AvailableUnitCount returns the count of input units
// once https://github.com/elastic/elastic-agent-shipper/issues/225 is delt with, we probably won't need this,
// as the primary use case for this is checking to see if the gRPC endpoint needs to be running.
// Once the input has a dedicated unit, we can use that.
func (c *UnitMap) AvailableUnitCount() int {
	c.mut.Lock()
	defer c.mut.Unlock()

	return len(c.inputUnits)
}

// DeleteUnit removes the given unit
func (c *UnitMap) DeleteUnit(unit *client.Unit) {
	if unit.Type() == client.UnitTypeOutput {
		c.outputUnit = nil
	} else {
		c.mut.Lock()
		defer c.mut.Unlock()
		delete(c.inputUnits, unit.ID())
	}
}

// GetUnit is another convenience method for fetching a given unit
func (c *UnitMap) GetUnit(id string, unitType client.UnitType) *client.Unit {
	c.mut.Lock()
	defer c.mut.Unlock()
	if unitType == client.UnitTypeOutput {
		return c.outputUnit
	} else {
		current, ok := c.inputUnits[id]
		if !ok {
			return nil
		}
		return current.Unit
	}
}

// SetOutput sets the output unit
func (c *UnitMap) SetOutput(unit *client.Unit) {
	c.mut.Lock()
	defer c.mut.Unlock()
	c.outputUnit = unit
}

// GetOutput returns the shipper output unit
// largely a convenience method
func (c *UnitMap) GetOutput() *client.Unit {
	c.mut.Lock()
	defer c.mut.Unlock()
	return c.outputUnit
}
