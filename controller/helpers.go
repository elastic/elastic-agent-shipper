// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"context"
	"fmt"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	cfglib "github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/logp/configure"
	"github.com/elastic/elastic-agent-libs/paths"
	"github.com/elastic/elastic-agent-shipper/config"
)

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

// initialize the global logging variables
func setLogging() error {
	wrapper := struct {
		Logging *cfglib.C `config:"logging"`
	}{}

	err := config.Overwrites.Unpack(&wrapper)
	if err != nil {
		return fmt.Errorf("error unpacking CLI overwrites for logger: %w", err)
	}

	err = configure.Logging("shipper", wrapper.Logging)
	if err != nil {
		return fmt.Errorf("error setting up logging config: %w", err)
	}
	return nil
}

// setPaths sets the global path variables
func setPaths() error {
	partialConfig := struct {
		Path paths.Path `config:"path"`
	}{}

	err := config.Overwrites.Unpack(&partialConfig)
	if err != nil {
		return fmt.Errorf("error extracting default paths: %w", err)
	}

	err = paths.InitPaths(&partialConfig.Path)
	if err != nil {
		return fmt.Errorf("error setting default paths: %w", err)
	}
	return nil
}
