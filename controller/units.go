// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"sync"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
)

// UnitMap is a wrapper for safely handling the map of V2 client units
type UnitMap struct {
	mut sync.Mutex
	// The map of input-type units
	inputUnits map[string]ShipperUnit
	outputUnit ShipperUnit
}

// ShipperUnit wraps the available config for a unit
type ShipperUnit struct {
	Unit *client.Unit
	// When we get an updated unit, we don't actually get a "new" unit event, just a pointer to the same unit, which is updated under the hood
	// which means anything in the parent unitMap hashmap is also updated.
	// if we want to compare configs, log levels, etc, we need to save it off
	config map[string]interface{}
}

// NewUnitMap creates a new Unit manager
func NewUnitMap() *UnitMap {
	return &UnitMap{
		inputUnits: make(map[string]ShipperUnit),
	}
}

// AddUnit adds a unit
func (c *UnitMap) AddUnit(unit *client.Unit) {
	c.mut.Lock()
	defer c.mut.Unlock()
	_, _, cfg := unit.Expected()
	if unit.Type() == client.UnitTypeOutput {
		c.outputUnit = ShipperUnit{Unit: unit, config: cfg.Source.AsMap()}
	} else {
		c.inputUnits[unit.ID()] = ShipperUnit{Unit: unit, config: cfg.Source.AsMap()}
	}

}

// UpdateUnit updates the unit with a new state
func (c *UnitMap) UpdateUnit(unit *client.Unit) {
	c.mut.Lock()
	defer c.mut.Unlock()
	_, _, cfg := unit.Expected()
	if unit.Type() == client.UnitTypeInput {
		// Probably a bug in elastic-agent if we hit this
		current, ok := c.inputUnits[unit.ID()]
		if !ok {
			return
		}
		current.config = cfg.Source.AsMap()
		c.inputUnits[unit.ID()] = current
	} else {
		c.outputUnit.config = cfg.Source.AsMap()
	}
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
		c.outputUnit = ShipperUnit{}
	} else {
		c.mut.Lock()
		defer c.mut.Unlock()
		delete(c.inputUnits, unit.ID())
	}
}

// GetInputUnit is another convenience method for fetching a given unit
func (c *UnitMap) GetInputUnit(id string, unitType client.UnitType) ShipperUnit {
	c.mut.Lock()
	defer c.mut.Unlock()
	if unitType == client.UnitTypeOutput {
		return c.outputUnit
	}
	current, ok := c.inputUnits[id]
	if !ok {
		return ShipperUnit{}
	}
	return current

}

// GetOutput returns the shipper output unit
// largely a convenience method
func (c *UnitMap) GetOutput() *client.Unit {
	c.mut.Lock()
	defer c.mut.Unlock()
	return c.outputUnit.Unit
}
