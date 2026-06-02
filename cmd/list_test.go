package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestListCommandTerminology(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want *cobra.Command
	}{
		{name: "projects", args: []string{"projects"}, want: listProjectsCmd},
		{name: "sites", args: []string{"sites"}, want: listSitesCmd},
		{name: "environments", args: []string{"environments"}, want: listEnvironmentsCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := listCmd.Find(tt.args)
			if err != nil {
				t.Fatalf("Find(%v) error = %v", tt.args, err)
			}
			if got != tt.want {
				t.Fatalf("Find(%v) = %q, want %q", tt.args, got.Use, tt.want.Use)
			}
		})
	}
}

func TestListSitesDoesNotAliasEnvironments(t *testing.T) {
	for _, alias := range listSitesCmd.Aliases {
		if alias == "environments" {
			t.Fatalf("list sites should not hide list environments behind an alias")
		}
	}
}
