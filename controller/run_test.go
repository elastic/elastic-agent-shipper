// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package controller

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper/config"
	"github.com/elastic/elastic-agent-shipper/output"
)

func TestUnmanaged(t *testing.T) {
	_ = logp.DevelopmentSetup()
	serverAddr := filepath.Join(os.TempDir(), fmt.Sprintf("test-unmanaged-%s.sock", uuid.Must(uuid.NewV4()).String()))
	cfg := config.DefaultConfig()
	cfg.Type = "console"
	cfg.Shipper.Output.Console = &output.ConsoleConfig{Enabled: true}
	cfg.Shipper.Server.Server = serverAddr

	defer func() {
		_ = os.Remove(serverAddr)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)

	go func() {
		err := RunUnmanaged(ctx, cfg)
		require.NoError(t, err)
	}()
	// wait a bit for the components to start
	time.Sleep(time.Millisecond * 300)
	// basic test, make sure output is running
	con, err := net.Dial("unix", serverAddr)
	defer func() {
		_ = con.Close()
	}()
	require.NoError(t, err)
	cancel()
}
