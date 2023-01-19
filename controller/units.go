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
	inputUnits map[unitKey]ShipperUnit
	outputUnit *client.Unit
}

// a unit ID isn't actually unique, the hash key must be a combination ID/type
type unitKey struct {
	ID   string
	Type client.UnitType
}

// ShipperUnit wraps the available config for a unit
type ShipperUnit struct {
	Unit *client.Unit
	// for now, we're treating the connection config for the unit a bit differently,
	// as it appears that the connection is the same across the lifetime of a unit
	Conn config.ShipperClientConfig
}

// NewUnitMap creates a new Unit manager
func NewUnitMap() *UnitMap {
	return &UnitMap{
		inputUnits: make(map[unitKey]ShipperUnit),
	}
}

// AddUnit adds a unit
func (c *UnitMap) AddUnit(unit *client.Unit, conn config.ShipperClientConfig) {
	c.mut.Lock()
	defer c.mut.Unlock()
	c.inputUnits[unitKey{ID: unit.ID(), Type: unit.Type()}] = ShipperUnit{Unit: unit, Conn: conn}

}

// UpdateUnit updates the unit with a new state
func (c *UnitMap) UpdateUnit(unit *client.Unit) {
	c.mut.Lock()
	defer c.mut.Unlock()
	// not entirely sure how safe this is; if we get a UnitChangedModified with a unit that doesn't exist,
	// is that just a bug in elastic-agent?
	current, ok := c.inputUnits[unitKey{ID: unit.ID(), Type: unit.Type()}]
	if !ok {
		return
	}
	current.Unit = unit
	c.inputUnits[unitKey{ID: unit.ID(), Type: unit.Type()}] = current
}

// ShipperHasConfig determines if the shipper has the needed config to start
func (c *UnitMap) ShipperHasConfig() bool {
	c.mut.Lock()
	defer c.mut.Unlock()
	return len(c.inputUnits) > 0 && c.outputUnit != nil
}

// ShipperConfig "bundles" the input and output config needed for the shipper to start.
func (c *UnitMap) ShipperConfig() (*client.Unit, config.ShipperClientConfig, bool) {
	c.mut.Lock()
	defer c.mut.Unlock()
	if c.outputUnit == nil {
		return nil, config.ShipperClientConfig{}, false
	}
	// doesn't actually matter which input we get the config from, they should all be the same.
	newUnit := config.ShipperClientConfig{}
	found := false
	for _, u := range c.inputUnits {
		newUnit = u.Conn
		found = true
	}
	if !found {
		return nil, config.ShipperClientConfig{}, false
	}

	return c.outputUnit, newUnit, true
}

// DeleteInput removes a unit, called after the Client sends a unit remove event
func (c *UnitMap) DeleteInput(unit *client.Unit) {
	c.mut.Lock()
	defer c.mut.Unlock()
	delete(c.inputUnits, unitKey{ID: unit.ID(), Type: unit.Type()})

}

// SetOutput sets the output unit
func (c *UnitMap) SetOutput(unit *client.Unit) {
	c.mut.Lock()
	defer c.mut.Unlock()
	c.outputUnit = unit
}

// DeleteOutput deletes the associated output unit
func (c *UnitMap) DeleteOutput() {
	c.outputUnit = nil
}

// GetOutput returns the shipper output unit
func (c *UnitMap) GetOutput() *client.Unit {
	c.mut.Lock()
	defer c.mut.Unlock()
	return c.outputUnit
}
