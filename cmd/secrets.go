package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	libopsv1 "github.com/libops/proto/libops/v1"
	"github.com/libops/sitectl-libops/pkg/api"
	"github.com/libops/sitectl-libops/pkg/resources"
	"github.com/spf13/cobra"
)

var createSecretsCmd = &cobra.Command{
	Use:     "secret",
	Aliases: []string{"secrets"},
	Short:   "Create a secret",
	Long:    "Create a secret for an organization, project, or site. Specify one of --organization-id, --project-id, or --site-id.",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}

		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		orgID, _ := cmd.Flags().GetString("organization-id")
		projectID, _ := cmd.Flags().GetString("project-id")
		siteID, _ := cmd.Flags().GetString("site-id")

		name, err := cmd.Flags().GetString("name")
		if err != nil {
			return err
		}

		value, err := secretValueFromFlags(cmd)
		if err != nil {
			return err
		}

		// Determine which endpoint to call based on which ID is provided
		if orgID != "" {
			resp, err := client.OrganizationSecretService.CreateOrganizationSecret(cmd.Context(), connect.NewRequest(&libopsv1.CreateOrganizationSecretRequest{
				OrganizationId: orgID,
				Name:           name,
				Value:          value,
			}))
			if err != nil {
				return fmt.Errorf("failed to create organization secret: %w", err)
			}
			fmt.Printf("✓ Created organization secret: %s\n", resp.Msg.Secret.SecretId)
			fmt.Printf("  Name: %s\n", resp.Msg.Secret.Name)
		} else if projectID != "" {
			resp, err := client.ProjectSecretService.CreateProjectSecret(cmd.Context(), connect.NewRequest(&libopsv1.CreateProjectSecretRequest{
				ProjectId: projectID,
				Name:      name,
				Value:     value,
			}))
			if err != nil {
				return fmt.Errorf("failed to create project secret: %w", err)
			}
			fmt.Printf("✓ Created project secret: %s\n", resp.Msg.Secret.SecretId)
			fmt.Printf("  Name: %s\n", resp.Msg.Secret.Name)
		} else if siteID != "" {
			resp, err := client.SiteSecretService.CreateSiteSecret(cmd.Context(), connect.NewRequest(&libopsv1.CreateSiteSecretRequest{
				SiteId: siteID,
				Name:   name,
				Value:  value,
			}))
			if err != nil {
				return fmt.Errorf("failed to create site secret: %w", err)
			}
			fmt.Printf("✓ Created site secret: %s\n", resp.Msg.Secret.SecretId)
			fmt.Printf("  Name: %s\n", resp.Msg.Secret.Name)
		} else {
			return fmt.Errorf("must specify one of --organization-id, --project-id, or --site-id")
		}

		return nil
	},
}

var listSecretsCmd = &cobra.Command{
	Use:     "secrets",
	Aliases: []string{"secret"},
	Short:   "List secrets",
	Long:    "List secrets. Optionally filter by --organization-id, --project-id, or --site-id. If no filter is specified, lists all secrets.",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}

		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		orgID, _ := cmd.Flags().GetString("organization-id")
		projectID, _ := cmd.Flags().GetString("project-id")
		siteID, _ := cmd.Flags().GetString("site-id")

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.TabIndent)
		fmt.Fprintln(w, "SECRET ID\tNAME\tSCOPE")
		fmt.Fprintln(w, "---------\t----\t-----")

		// If specific ID is provided, query that endpoint
		if orgID != "" {
			resp, err := client.OrganizationSecretService.ListOrganizationSecrets(cmd.Context(), connect.NewRequest(&libopsv1.ListOrganizationSecretsRequest{
				OrganizationId: orgID,
			}))
			if err != nil {
				return fmt.Errorf("failed to list organization secrets: %w", err)
			}
			for _, s := range resp.Msg.Secrets {
				fmt.Fprintf(w, "%s\t%s\torg:%s\n", s.SecretId, s.Name, orgID)
			}
		} else if projectID != "" {
			resp, err := client.ProjectSecretService.ListProjectSecrets(cmd.Context(), connect.NewRequest(&libopsv1.ListProjectSecretsRequest{
				ProjectId: projectID,
			}))
			if err != nil {
				return fmt.Errorf("failed to list project secrets: %w", err)
			}
			for _, s := range resp.Msg.Secrets {
				fmt.Fprintf(w, "%s\t%s\tproject:%s\n", s.SecretId, s.Name, projectID)
			}
		} else if siteID != "" {
			resp, err := client.SiteSecretService.ListSiteSecrets(cmd.Context(), connect.NewRequest(&libopsv1.ListSiteSecretsRequest{
				SiteId: siteID,
			}))
			if err != nil {
				return fmt.Errorf("failed to list site secrets: %w", err)
			}
			for _, s := range resp.Msg.Secrets {
				fmt.Fprintf(w, "%s\t%s\tsite:%s\n", s.SecretId, s.Name, siteID)
			}
		} else {
			// List all - use shared resource functions with caching
			noCache, _ := cmd.Flags().GetBool("no-cache")
			useCache := !noCache

			// List organization secrets
			orgs, err := resources.ListOrganizations(cmd.Context(), apiBaseURL, useCache)
			if err != nil {
				slog.Warn("Failed to list organizations", "err", err)
			} else {
				for _, org := range orgs {
					orgSecretsResp, err := client.OrganizationSecretService.ListOrganizationSecrets(cmd.Context(), connect.NewRequest(&libopsv1.ListOrganizationSecretsRequest{
						OrganizationId: org.OrganizationId,
					}))
					if err != nil {
						slog.Warn("Failed to list secrets for organization", "org_id", org.OrganizationId, "err", err)
						continue
					}
					for _, s := range orgSecretsResp.Msg.Secrets {
						fmt.Fprintf(w, "%s\t%s\torg:%s\n", s.SecretId, s.Name, org.OrganizationId)
					}
				}
			}

			// List project secrets
			projects, err := resources.ListProjects(cmd.Context(), apiBaseURL, useCache, nil)
			if err != nil {
				slog.Warn("Failed to list projects", "err", err)
			} else {
				for _, proj := range projects {
					projSecretsResp, err := client.ProjectSecretService.ListProjectSecrets(cmd.Context(), connect.NewRequest(&libopsv1.ListProjectSecretsRequest{
						ProjectId: proj.ProjectId,
					}))
					if err != nil {
						slog.Warn("Failed to list secrets for project", "project_id", proj.ProjectId, "err", err)
						continue
					}
					for _, s := range projSecretsResp.Msg.Secrets {
						fmt.Fprintf(w, "%s\t%s\tproject:%s\n", s.SecretId, s.Name, proj.ProjectId)
					}
				}
			}

			// List site secrets
			sites, err := resources.ListSites(cmd.Context(), apiBaseURL, useCache, nil, nil)
			if err != nil {
				slog.Warn("Failed to list sites", "err", err)
			} else {
				for _, site := range sites {
					siteSecretsResp, err := client.SiteSecretService.ListSiteSecrets(cmd.Context(), connect.NewRequest(&libopsv1.ListSiteSecretsRequest{
						SiteId: site.SiteId,
					}))
					if err != nil {
						slog.Warn("Failed to list secrets for site", "site_id", site.SiteId, "err", err)
						continue
					}
					for _, s := range siteSecretsResp.Msg.Secrets {
						fmt.Fprintf(w, "%s\t%s\tsite:%s\n", s.SecretId, s.Name, site.SiteId)
					}
				}
			}
		}

		return w.Flush()
	},
}

var editSecretCmd = &cobra.Command{
	Use:   "secret <secret-id>",
	Short: "Update a secret value",
	Long:  "Update a secret value for an organization, project, or site. Specify one of --organization-id, --project-id, or --site-id.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		value, err := secretValueFromFlags(cmd)
		if err != nil {
			return err
		}

		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		orgID, _ := cmd.Flags().GetString("organization-id")
		projectID, _ := cmd.Flags().GetString("project-id")
		siteID, _ := cmd.Flags().GetString("site-id")
		secretID := args[0]
		updateMask := &fieldmaskpb.FieldMask{Paths: []string{"value"}}

		if orgID != "" {
			resp, err := client.OrganizationSecretService.UpdateOrganizationSecret(cmd.Context(), connect.NewRequest(&libopsv1.UpdateOrganizationSecretRequest{
				OrganizationId: orgID,
				SecretId:       secretID,
				Value:          &value,
				UpdateMask:     updateMask,
			}))
			if err != nil {
				return fmt.Errorf("failed to update organization secret: %w", err)
			}
			fmt.Printf("Updated organization secret: %s (%s)\n", resp.Msg.Secret.SecretId, resp.Msg.Secret.Name)
			return nil
		}
		if projectID != "" {
			resp, err := client.ProjectSecretService.UpdateProjectSecret(cmd.Context(), connect.NewRequest(&libopsv1.UpdateProjectSecretRequest{
				ProjectId:  projectID,
				SecretId:   secretID,
				Value:      &value,
				UpdateMask: updateMask,
			}))
			if err != nil {
				return fmt.Errorf("failed to update project secret: %w", err)
			}
			fmt.Printf("Updated project secret: %s (%s)\n", resp.Msg.Secret.SecretId, resp.Msg.Secret.Name)
			return nil
		}
		if siteID != "" {
			resp, err := client.SiteSecretService.UpdateSiteSecret(cmd.Context(), connect.NewRequest(&libopsv1.UpdateSiteSecretRequest{
				SiteId:     siteID,
				SecretId:   secretID,
				Value:      &value,
				UpdateMask: updateMask,
			}))
			if err != nil {
				return fmt.Errorf("failed to update site secret: %w", err)
			}
			fmt.Printf("Updated site secret: %s (%s)\n", resp.Msg.Secret.SecretId, resp.Msg.Secret.Name)
			return nil
		}
		return fmt.Errorf("must specify one of --organization-id, --project-id, or --site-id")
	},
}

var deleteSecretCmd = &cobra.Command{
	Use:   "secret <secret-id>",
	Short: "Delete a secret",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		secretID := args[0]
		confirmed, err := confirmDeletion(cmd, "secret", secretID)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Deletion cancelled.")
			return nil
		}

		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		orgID, _ := cmd.Flags().GetString("organization-id")
		projectID, _ := cmd.Flags().GetString("project-id")
		siteID, _ := cmd.Flags().GetString("site-id")

		if orgID != "" {
			_, err = client.OrganizationSecretService.DeleteOrganizationSecret(cmd.Context(), connect.NewRequest(&libopsv1.DeleteOrganizationSecretRequest{
				OrganizationId: orgID,
				SecretId:       secretID,
			}))
		} else if projectID != "" {
			_, err = client.ProjectSecretService.DeleteProjectSecret(cmd.Context(), connect.NewRequest(&libopsv1.DeleteProjectSecretRequest{
				ProjectId: projectID,
				SecretId:  secretID,
			}))
		} else if siteID != "" {
			_, err = client.SiteSecretService.DeleteSiteSecret(cmd.Context(), connect.NewRequest(&libopsv1.DeleteSiteSecretRequest{
				SiteId:   siteID,
				SecretId: secretID,
			}))
		} else {
			return fmt.Errorf("must specify one of --organization-id, --project-id, or --site-id")
		}
		if err != nil {
			return fmt.Errorf("failed to delete secret: %w", err)
		}
		fmt.Printf("Deleted secret: %s\n", secretID)
		return nil
	},
}

func secretValueFromFlags(cmd *cobra.Command) (string, error) {
	value, _ := cmd.Flags().GetString("value")
	valueFile, _ := cmd.Flags().GetString("value-file")
	if strings.TrimSpace(value) != "" && strings.TrimSpace(valueFile) != "" {
		return "", fmt.Errorf("specify only one of --value or --value-file")
	}
	if strings.TrimSpace(valueFile) != "" {
		data, err := os.ReadFile(valueFile) // #nosec G304 -- user explicitly selects the secret value file.
		if err != nil {
			return "", fmt.Errorf("read value file: %w", err)
		}
		value = strings.TrimRight(string(data), "\r\n")
	}
	if value == "" {
		return "", fmt.Errorf("must specify --value or --value-file")
	}
	return value, nil
}

func init() {
	// Add secrets subcommand to create command
	createCmd.AddCommand(createSecretsCmd)
	createSecretsCmd.Flags().String("organization-id", "", "Organization ID")
	createSecretsCmd.Flags().String("project-id", "", "Project ID")
	createSecretsCmd.Flags().String("site-id", "", "Site ID")
	createSecretsCmd.Flags().String("name", "", "Secret name (required)")
	createSecretsCmd.Flags().String("value", "", "Secret value")
	createSecretsCmd.Flags().String("value-file", "", "Path to a file containing the secret value")
	_ = createSecretsCmd.MarkFlagRequired("name")
	createSecretsCmd.MarkFlagsOneRequired("organization-id", "project-id", "site-id")
	createSecretsCmd.MarkFlagsMutuallyExclusive("organization-id", "project-id", "site-id")

	// Add secrets subcommand to list command
	listCmd.AddCommand(listSecretsCmd)
	listSecretsCmd.Flags().String("organization-id", "", "Filter by organization ID")
	listSecretsCmd.Flags().String("project-id", "", "Filter by project ID")
	listSecretsCmd.Flags().String("site-id", "", "Filter by site ID")
	listSecretsCmd.MarkFlagsMutuallyExclusive("organization-id", "project-id", "site-id")

	editCmd.AddCommand(editSecretCmd)
	editSecretCmd.Flags().String("organization-id", "", "Organization ID")
	editSecretCmd.Flags().String("project-id", "", "Project ID")
	editSecretCmd.Flags().String("site-id", "", "Site ID")
	editSecretCmd.Flags().String("value", "", "Secret value")
	editSecretCmd.Flags().String("value-file", "", "Path to a file containing the secret value")
	editSecretCmd.MarkFlagsOneRequired("organization-id", "project-id", "site-id")
	editSecretCmd.MarkFlagsMutuallyExclusive("organization-id", "project-id", "site-id")

	deleteCmd.AddCommand(deleteSecretCmd)
	deleteSecretCmd.Flags().String("organization-id", "", "Organization ID")
	deleteSecretCmd.Flags().String("project-id", "", "Project ID")
	deleteSecretCmd.Flags().String("site-id", "", "Site ID")
	deleteSecretCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	deleteSecretCmd.MarkFlagsOneRequired("organization-id", "project-id", "site-id")
	deleteSecretCmd.MarkFlagsMutuallyExclusive("organization-id", "project-id", "site-id")
}
