package main

import (
	"log/slog"

	"github.com/libops/sitectl-libops/cmd"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
)

func main() {
	// Create plugin SDK with metadata
	sdk := plugin.NewSDK(plugin.Metadata{
		Name:        "libops",
		Version:     "1.0.0",
		Description: "Interact with the Libops API",
		Author:      "Libops Team",
	})

	// Add the metadata command for plugin discovery
	sdk.AddCommand(sdk.GetMetadataCommand())

	// Get current context for libops flags
	c, err := config.Current()
	if err != nil {
		slog.Warn("Unable to fetch current context", "err", err)
		c = ""
	}

	// Add libops-specific flags
	sdk.AddLibopsFlags(c)

	// Add all libops commands
	cmd.RegisterCommands(sdk)

	// Execute the plugin
	sdk.Execute()
}
