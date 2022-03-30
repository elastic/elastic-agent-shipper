// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"github.com/magefile/mage/sh"

	// mage:import
	_ "github.com/elastic/elastic-agent/dev-tools/mage/target/common"
)

func GenProto() {
	sh.Run("protoc", "-I", " shipper", "-I", "shipper/vendor", "--go_out=./shipper", "--go-grpc_out=./shipper", "--go_opt=paths=source_relative", "--go-grpc_opt=paths=source_relative", "shipper/shipper.proto")
}

func Build() {
	sh.Run("go", "build")
}
