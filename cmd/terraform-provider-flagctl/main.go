// Command terraform-provider-flagctl serves the Terraform provider over plugin RPC.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ahmedr1zwan/flagctl/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// Set by release builds; local development builds deliberately have no release version.
var version = "dev"

func main() {
	if err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/ahmedr1zwan/flagctl",
	}); err != nil {
		fmt.Fprintln(os.Stderr, "could not start flagctl Terraform provider")
		os.Exit(1)
	}
}
