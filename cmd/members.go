package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	libopsv1 "github.com/libops/proto/libops/v1"
	"github.com/libops/sitectl-libops/pkg/api"
	"github.com/libops/sitectl-libops/pkg/resources"
	"github.com/spf13/cobra"
)

var createMembersCmd = &cobra.Command{
	Use:     "members",
	Aliases: []string{"member"},
	Short:   "Add a member",
	Long:    "Add a member to an organization, project, or site. Specify one of --organization-id, --project-id, or --site-id.",
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

		accountEmail, err := cmd.Flags().GetString("account-id")
		if err != nil {
			return err
		}

		role, err := cmd.Flags().GetString("role")
		if err != nil {
			return err
		}

		// Determine which endpoint to call based on which ID is provided
		if orgID != "" {
			resp, err := client.MemberService.CreateOrganizationMember(cmd.Context(), connect.NewRequest(&libopsv1.CreateOrganizationMemberRequest{
				OrganizationId: orgID,
				Email:          accountEmail,
				Role:           role,
			}))
			if err != nil {
				return fmt.Errorf("failed to create organization member: %w", err)
			}
			fmt.Printf("✓ Added member to organization\n")
			fmt.Printf("  Account ID: %s\n", resp.Msg.Member.AccountId)
			fmt.Printf("  Role: %s\n", resp.Msg.Member.Role)
		} else if projectID != "" {
			resp, err := client.ProjectMemberService.CreateProjectMember(cmd.Context(), connect.NewRequest(&libopsv1.CreateProjectMemberRequest{
				ProjectId: projectID,
				Email:     accountEmail,
				Role:      role,
			}))
			if err != nil {
				return fmt.Errorf("failed to create project member: %w", err)
			}
			fmt.Printf("✓ Added member to project\n")
			fmt.Printf("  Account ID: %s\n", resp.Msg.Member.AccountId)
			fmt.Printf("  Role: %s\n", resp.Msg.Member.Role)
		} else if siteID != "" {
			resp, err := client.SiteMemberService.CreateSiteMember(cmd.Context(), connect.NewRequest(&libopsv1.CreateSiteMemberRequest{
				SiteId: siteID,
				Email:  accountEmail,
				Role:   role,
			}))
			if err != nil {
				return fmt.Errorf("failed to create site member: %w", err)
			}
			fmt.Printf("✓ Added member to site\n")
			fmt.Printf("  Account ID: %s\n", resp.Msg.Member.AccountId)
			fmt.Printf("  Role: %s\n", resp.Msg.Member.Role)
		} else {
			return fmt.Errorf("must specify one of --organization-id, --project-id, or --site-id")
		}

		return nil
	},
}

var listMembersCmd = &cobra.Command{
	Use:     "members",
	Aliases: []string{"member"},
	Short:   "List members",
	Long:    "List members. Optionally filter by --organization-id, --project-id, or --site-id. If no filter is specified, lists all members.",
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
		fmt.Fprintln(w, "ACCOUNT ID\tEMAIL\tNAME\tROLE\tSTATUS\tSCOPE")
		fmt.Fprintln(w, "----------\t-----\t----\t----\t------\t-----")

		// If specific ID is provided, query that endpoint
		if orgID != "" {
			resp, err := client.MemberService.ListOrganizationMembers(cmd.Context(), connect.NewRequest(&libopsv1.ListOrganizationMembersRequest{
				OrganizationId: orgID,
			}))
			if err != nil {
				return fmt.Errorf("failed to list organization members: %w", err)
			}
			for _, m := range resp.Msg.Members {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\torg:%s\n", m.AccountId, m.Email, m.Name, m.Role, m.Status, orgID)
			}
		} else if projectID != "" {
			resp, err := client.ProjectMemberService.ListProjectMembers(cmd.Context(), connect.NewRequest(&libopsv1.ListProjectMembersRequest{
				ProjectId: projectID,
			}))
			if err != nil {
				return fmt.Errorf("failed to list project members: %w", err)
			}
			for _, m := range resp.Msg.Members {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\tproject:%s\n", m.AccountId, m.Email, m.Name, m.Role, m.Status, projectID)
			}
		} else if siteID != "" {
			resp, err := client.SiteMemberService.ListSiteMembers(cmd.Context(), connect.NewRequest(&libopsv1.ListSiteMembersRequest{
				SiteId: siteID,
			}))
			if err != nil {
				return fmt.Errorf("failed to list site members: %w", err)
			}
			for _, m := range resp.Msg.Members {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\tsite:%s\n", m.AccountId, m.Email, m.Name, m.Role, m.Status, siteID)
			}
		} else {
			// List all - use shared resource functions with caching
			noCache, _ := cmd.Flags().GetBool("no-cache")
			useCache := !noCache

			// List organization members
			orgs, err := resources.ListOrganizations(cmd.Context(), apiBaseURL, useCache)
			if err != nil {
				slog.Warn("Failed to list organizations", "err", err)
			} else {
				for _, org := range orgs {
					orgMembersResp, err := client.MemberService.ListOrganizationMembers(cmd.Context(), connect.NewRequest(&libopsv1.ListOrganizationMembersRequest{
						OrganizationId: org.OrganizationId,
					}))
					if err != nil {
						slog.Warn("Failed to list members for organization", "org_id", org.OrganizationId, "err", err)
						continue
					}
					for _, m := range orgMembersResp.Msg.Members {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\torg:%s\n", m.AccountId, m.Email, m.Name, m.Role, m.Status, org.OrganizationId)
					}
				}
			}

			// List project members
			projects, err := resources.ListProjects(cmd.Context(), apiBaseURL, useCache, nil)
			if err != nil {
				slog.Warn("Failed to list projects", "err", err)
			} else {
				for _, proj := range projects {
					projMembersResp, err := client.ProjectMemberService.ListProjectMembers(cmd.Context(), connect.NewRequest(&libopsv1.ListProjectMembersRequest{
						ProjectId: proj.ProjectId,
					}))
					if err != nil {
						slog.Warn("Failed to list members for project", "project_id", proj.ProjectId, "err", err)
						continue
					}
					for _, m := range projMembersResp.Msg.Members {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\tproject:%s\n", m.AccountId, m.Email, m.Name, m.Role, m.Status, proj.ProjectId)
					}
				}
			}

			// List site members
			sites, err := resources.ListSites(cmd.Context(), apiBaseURL, useCache, nil, nil)
			if err != nil {
				slog.Warn("Failed to list sites", "err", err)
			} else {
				for _, site := range sites {
					siteMembersResp, err := client.SiteMemberService.ListSiteMembers(cmd.Context(), connect.NewRequest(&libopsv1.ListSiteMembersRequest{
						SiteId: site.SiteId,
					}))
					if err != nil {
						slog.Warn("Failed to list members for site", "site_id", site.SiteId, "err", err)
						continue
					}
					for _, m := range siteMembersResp.Msg.Members {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\tsite:%s\n", m.AccountId, m.Email, m.Name, m.Role, m.Status, site.SiteId)
					}
				}
			}
		}

		return w.Flush()
	},
}

var editMemberCmd = &cobra.Command{
	Use:   "member <account-id>",
	Short: "Update a member role",
	Long:  "Update a member role for an organization, project, or site. Specify one of --organization-id, --project-id, or --site-id.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		role, err := cmd.Flags().GetString("role")
		if err != nil {
			return err
		}
		if role == "" {
			return fmt.Errorf("--role is required")
		}

		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		accountID := args[0]
		orgID, _ := cmd.Flags().GetString("organization-id")
		projectID, _ := cmd.Flags().GetString("project-id")
		siteID, _ := cmd.Flags().GetString("site-id")
		updateMask := &fieldmaskpb.FieldMask{Paths: []string{"role"}}

		if orgID != "" {
			resp, err := client.MemberService.UpdateOrganizationMember(cmd.Context(), connect.NewRequest(&libopsv1.UpdateOrganizationMemberRequest{
				OrganizationId: orgID,
				AccountId:      accountID,
				Role:           role,
				UpdateMask:     updateMask,
			}))
			if err != nil {
				return fmt.Errorf("failed to update organization member: %w", err)
			}
			fmt.Printf("Updated organization member: %s (%s)\n", resp.Msg.Member.AccountId, resp.Msg.Member.Role)
			return nil
		}
		if projectID != "" {
			resp, err := client.ProjectMemberService.UpdateProjectMember(cmd.Context(), connect.NewRequest(&libopsv1.UpdateProjectMemberRequest{
				ProjectId:  projectID,
				AccountId:  accountID,
				Role:       role,
				UpdateMask: updateMask,
			}))
			if err != nil {
				return fmt.Errorf("failed to update project member: %w", err)
			}
			fmt.Printf("Updated project member: %s (%s)\n", resp.Msg.Member.AccountId, resp.Msg.Member.Role)
			return nil
		}
		if siteID != "" {
			resp, err := client.SiteMemberService.UpdateSiteMember(cmd.Context(), connect.NewRequest(&libopsv1.UpdateSiteMemberRequest{
				SiteId:     siteID,
				AccountId:  accountID,
				Role:       role,
				UpdateMask: updateMask,
			}))
			if err != nil {
				return fmt.Errorf("failed to update site member: %w", err)
			}
			fmt.Printf("Updated site member: %s (%s)\n", resp.Msg.Member.AccountId, resp.Msg.Member.Role)
			return nil
		}
		return fmt.Errorf("must specify one of --organization-id, --project-id, or --site-id")
	},
}

var deleteMemberCmd = &cobra.Command{
	Use:   "member <account-id>",
	Short: "Remove a member",
	Long:  "Remove a member from an organization, project, or site. Specify one of --organization-id, --project-id, or --site-id.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountID := args[0]
		confirmed, err := confirmDeletion(cmd, "member", accountID)
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
			_, err = client.MemberService.DeleteOrganizationMember(cmd.Context(), connect.NewRequest(&libopsv1.DeleteOrganizationMemberRequest{
				OrganizationId: orgID,
				AccountId:      accountID,
			}))
		} else if projectID != "" {
			_, err = client.ProjectMemberService.DeleteProjectMember(cmd.Context(), connect.NewRequest(&libopsv1.DeleteProjectMemberRequest{
				ProjectId: projectID,
				AccountId: accountID,
			}))
		} else if siteID != "" {
			_, err = client.SiteMemberService.DeleteSiteMember(cmd.Context(), connect.NewRequest(&libopsv1.DeleteSiteMemberRequest{
				SiteId:    siteID,
				AccountId: accountID,
			}))
		} else {
			return fmt.Errorf("must specify one of --organization-id, --project-id, or --site-id")
		}
		if err != nil {
			return fmt.Errorf("failed to remove member: %w", err)
		}

		fmt.Printf("Removed member: %s\n", accountID)
		return nil
	},
}

func init() {
	// Add members subcommand to create command
	createCmd.AddCommand(createMembersCmd)
	createMembersCmd.Flags().String("organization-id", "", "Organization ID")
	createMembersCmd.Flags().String("project-id", "", "Project ID")
	createMembersCmd.Flags().String("site-id", "", "Site ID")
	createMembersCmd.Flags().String("account-id", "", "Account email to add (required)")
	createMembersCmd.Flags().String("role", "read", "Role (owner, developer, read)")
	_ = createMembersCmd.MarkFlagRequired("account-id")
	createMembersCmd.MarkFlagsOneRequired("organization-id", "project-id", "site-id")
	createMembersCmd.MarkFlagsMutuallyExclusive("organization-id", "project-id", "site-id")

	// Add members subcommand to list command
	listCmd.AddCommand(listMembersCmd)
	listMembersCmd.Flags().String("organization-id", "", "Filter by organization ID")
	listMembersCmd.Flags().String("project-id", "", "Filter by project ID")
	listMembersCmd.Flags().String("site-id", "", "Filter by site ID")
	listMembersCmd.MarkFlagsMutuallyExclusive("organization-id", "project-id", "site-id")

	editCmd.AddCommand(editMemberCmd)
	editMemberCmd.Flags().String("organization-id", "", "Organization ID")
	editMemberCmd.Flags().String("project-id", "", "Project ID")
	editMemberCmd.Flags().String("site-id", "", "Site ID")
	editMemberCmd.Flags().String("role", "", "Role (owner, developer, read)")
	_ = editMemberCmd.MarkFlagRequired("role")
	editMemberCmd.MarkFlagsOneRequired("organization-id", "project-id", "site-id")
	editMemberCmd.MarkFlagsMutuallyExclusive("organization-id", "project-id", "site-id")

	deleteCmd.AddCommand(deleteMemberCmd)
	deleteMemberCmd.Flags().String("organization-id", "", "Organization ID")
	deleteMemberCmd.Flags().String("project-id", "", "Project ID")
	deleteMemberCmd.Flags().String("site-id", "", "Site ID")
	deleteMemberCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	deleteMemberCmd.MarkFlagsOneRequired("organization-id", "project-id", "site-id")
	deleteMemberCmd.MarkFlagsMutuallyExclusive("organization-id", "project-id", "site-id")
}
