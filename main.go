package main

import (
	"fmt"

	"github.com/libops/sitectl-libops/cmd"
	"github.com/libops/sitectl/pkg/plugin"
)

const defaultAPIURL = "https://api.libops.io"

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Create plugin SDK with metadata
	sdk := plugin.NewSDK(plugin.Metadata{
		Name:        "libops",
		Version:     fmt.Sprintf("%s (Built on %s from Git SHA %s)", version, date, commit),
		Description: "Interact with the Libops API",
		Author:      "Libops Team",
	})

	sdk.RootCmd.PersistentFlags().String("api-url", defaultAPIURL, "Base URL of the LibOps API")

	// Add all libops commands
	cmd.RegisterCommands(sdk)

	// Execute the plugin
	sdk.Execute()
}
