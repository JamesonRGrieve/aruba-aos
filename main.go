// SPDX-License-Identifier: AGPL-3.0-or-later

// Command aruba-aos is the OpenTofu/Terraform provider plugin entrypoint for
// ArubaOS-Switch (AOS-S) switches via the REST API v8.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/JamesonRGrieve/aruba-aos/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/jamesonrgrieve/aruba-aos",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
