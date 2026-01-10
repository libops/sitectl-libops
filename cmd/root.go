package cmd

import (
	"github.com/libops/sitectl/pkg/plugin"
)

// RegisterCommands registers all libops commands with the plugin SDK
func RegisterCommands(sdk *plugin.SDK) {
	// Authentication commands
	sdk.AddCommand(loginCmd)
	sdk.AddCommand(logoutCmd)
	sdk.AddCommand(whoamiCmd)

	// Resource management commands (CRUD)
	// Subcommands are automatically registered via init() functions
	sdk.AddCommand(createCmd)
	sdk.AddCommand(getCmd)
	sdk.AddCommand(listCmd)
	sdk.AddCommand(editCmd)
	sdk.AddCommand(deleteCmd)

	// Other commands
	sdk.AddCommand(composeCmd)
	sdk.AddCommand(portForwardCmd)
}
