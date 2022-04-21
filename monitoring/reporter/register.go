// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package reporter

import (
	"fmt"
	"sync"

	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
)

//The register defines a global interface for managing the various metrics outputs
//This might be overkill for what we have now, but I'd like to at least try to prepare
//for the fact that we'll have more outputs in the future.

//OutputInit is the function signature for any new output to initialize itself
type OutputInit func(config.Namespace) (Reporter, error)

type outputReg struct {
	outMap map[string]OutputInit
	mut    sync.Mutex
}

var globalOutputReg outputReg

func init() {
	globalOutputReg = outputReg{
		outMap: make(map[string]OutputInit),
		mut:    sync.Mutex{},
	}
}

//RegisterOutput adds a new metrics output to the global registry
func RegisterOutput(name string, init OutputInit) {
	globalOutputReg.mut.Lock()
	defer globalOutputReg.mut.Unlock()
	//check to make sure we're not overwriting anything
	_, ok := globalOutputReg.outMap[name]
	if ok {
		logp.L().Panicf("Tried to re-register queue metrics output with name %s", name)
	}
	globalOutputReg.outMap[name] = init
}

//OutputForName returns a given output init function for the name provided. Will return error if no input exists.
func OutputForName(name string) (OutputInit, error) {
	globalOutputReg.mut.Lock()
	defer globalOutputReg.mut.Unlock()
	init, ok := globalOutputReg.outMap[name]
	if !ok {
		return nil, fmt.Errorf("output %s does not exist", name)
	}
	return init, nil
}

//GetOutputList returns a list of registered outputs for debugging
func GetOutputList() []string {
	globalOutputReg.mut.Lock()
	defer globalOutputReg.mut.Unlock()

	outList := []string{}
	for k := range globalOutputReg.outMap {
		outList = append(outList, k)
	}
	return outList
}
