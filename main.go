// terraform-provider-zone manages zone.eu DNS through the ZoneID API v2.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/AlphaTurn/terraform-provider-zone/internal/provider"
)

// version is replaced at release time by the linker.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers such as delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/AlphaTurn/zone",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
