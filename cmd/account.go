package cmd

import (
	"fmt"
	"strings"

	"github.com/libops/sitectl-libops/pkg/api"
	"github.com/spf13/cobra"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Manage your LibOps account",
}

var accountUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update your account profile",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}

		var update api.AccountUpdate
		if cmd.Flags().Changed("name") {
			name, _ := cmd.Flags().GetString("name")
			name = strings.TrimSpace(name)
			update.Name = &name
		}
		if cmd.Flags().Changed("github-username") {
			githubUsername, _ := cmd.Flags().GetString("github-username")
			githubUsername = strings.TrimSpace(githubUsername)
			update.GithubUsername = &githubUsername
		}
		if update.Name == nil && update.GithubUsername == nil {
			return fmt.Errorf("no fields to update - specify --name or --github-username")
		}

		account, err := api.UpdateCurrentAccount(cmd.Context(), apiBaseURL, update)
		if err != nil {
			return err
		}

		fmt.Printf("Updated account: %s\n", account.ID)
		fmt.Printf("Email: %s\n", account.Email)
		if account.Name != "" {
			fmt.Printf("Name: %s\n", account.Name)
		}
		if account.GithubUsername != "" {
			fmt.Printf("GitHub Username: %s\n", account.GithubUsername)
		} else {
			fmt.Println("GitHub Username: missing")
		}
		return nil
	},
}

func init() {
	accountCmd.AddCommand(accountUpdateCmd)
	accountUpdateCmd.Flags().String("name", "", "Display name")
	accountUpdateCmd.Flags().String("github-username", "", "GitHub username used for repository access")
}
