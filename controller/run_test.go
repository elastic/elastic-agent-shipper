// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
	"github.com/elastic/elastic-agent-shipper/output"
	"github.com/elastic/elastic-agent-shipper/tools"
)

func TestUnmanaged(t *testing.T) {
	_ = logp.DevelopmentSetup()
	serverAddr := tools.GenerateTestAddr(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Type = "console"
	cfg.Monitoring = false
	cfg.Shipper.Output.Console = &output.ConsoleConfig{Enabled: true}
	cfg.Shipper.Server.Server = serverAddr

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)

	go func() {
		err := RunUnmanaged(ctx, cfg)
		require.NoError(t, err)
	}()
	// wait a bit for the components to start
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out trying to dial %s", serverAddr)
		default:
		}
		con, err := tools.DialTestAddr(serverAddr)
		if err != nil && (strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "cannot find the file")) {
			continue
		}
		require.NoError(t, err)
		t.Cleanup(func() { _ = con.Close() })
		break
	}
	cancel()
}
