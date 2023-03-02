// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build integration && !windows

package integration

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func getDialOptions() []grpc.DialOption {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	return opts
}
