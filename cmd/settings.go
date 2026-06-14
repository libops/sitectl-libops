package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"connectrpc.com/connect"
	libopsv1 "github.com/libops/proto/libops/v1"
	"github.com/libops/sitectl-libops/pkg/api"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

var createSettingCmd = &cobra.Command{
	Use:   "setting",
	Short: "Create a setting",
	Long:  "Create a setting for an organization, project, or site. Specify one of --organization-id, --project-id, or --site-id.",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		key, _ := cmd.Flags().GetString("key")
		value, _ := cmd.Flags().GetString("value")
		description, _ := cmd.Flags().GetString("description")
		editable, _ := cmd.Flags().GetBool("editable")

		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}
		orgID, projectID, siteID := settingScopeFlags(cmd)

		if orgID != "" {
			resp, err := client.OrganizationSettingService.CreateOrganizationSetting(cmd.Context(), connect.NewRequest(&libopsv1.CreateOrganizationSettingRequest{
				OrganizationId: orgID,
				Key:            key,
				Value:          value,
				Editable:       editable,
				Description:    description,
			}))
			if err != nil {
				return fmt.Errorf("failed to create organization setting: %w", err)
			}
			fmt.Printf("Created organization setting: %s (%s)\n", resp.Msg.Setting.SettingId, resp.Msg.Setting.Key)
			return nil
		}
		if projectID != "" {
			resp, err := client.ProjectSettingService.CreateProjectSetting(cmd.Context(), connect.NewRequest(&libopsv1.CreateProjectSettingRequest{
				ProjectId:   projectID,
				Key:         key,
				Value:       value,
				Editable:    editable,
				Description: description,
			}))
			if err != nil {
				return fmt.Errorf("failed to create project setting: %w", err)
			}
			fmt.Printf("Created project setting: %s (%s)\n", resp.Msg.Setting.SettingId, resp.Msg.Setting.Key)
			return nil
		}
		if siteID != "" {
			resp, err := client.SiteSettingService.CreateSiteSetting(cmd.Context(), connect.NewRequest(&libopsv1.CreateSiteSettingRequest{
				SiteId:      siteID,
				Key:         key,
				Value:       value,
				Editable:    editable,
				Description: description,
			}))
			if err != nil {
				return fmt.Errorf("failed to create site setting: %w", err)
			}
			fmt.Printf("Created site setting: %s (%s)\n", resp.Msg.Setting.SettingId, resp.Msg.Setting.Key)
			return nil
		}
		return fmt.Errorf("must specify one of --organization-id, --project-id, or --site-id")
	},
}

var listSettingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "List settings",
	Long:  "List settings for an organization, project, or site. Specify one of --organization-id, --project-id, or --site-id.",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}
		orgID, projectID, siteID := settingScopeFlags(cmd)

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SETTING ID\tKEY\tVALUE\tEDITABLE\tSTATUS\tSCOPE")

		if orgID != "" {
			resp, err := client.OrganizationSettingService.ListOrganizationSettings(cmd.Context(), connect.NewRequest(&libopsv1.ListOrganizationSettingsRequest{
				OrganizationId: orgID,
			}))
			if err != nil {
				return fmt.Errorf("failed to list organization settings: %w", err)
			}
			for _, setting := range resp.Msg.Settings {
				fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%s\torg:%s\n", setting.SettingId, setting.Key, setting.Value, setting.Editable, setting.Status, orgID)
			}
		} else if projectID != "" {
			resp, err := client.ProjectSettingService.ListProjectSettings(cmd.Context(), connect.NewRequest(&libopsv1.ListProjectSettingsRequest{
				ProjectId: projectID,
			}))
			if err != nil {
				return fmt.Errorf("failed to list project settings: %w", err)
			}
			for _, setting := range resp.Msg.Settings {
				fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%s\tproject:%s\n", setting.SettingId, setting.Key, setting.Value, setting.Editable, setting.Status, projectID)
			}
		} else if siteID != "" {
			resp, err := client.SiteSettingService.ListSiteSettings(cmd.Context(), connect.NewRequest(&libopsv1.ListSiteSettingsRequest{
				SiteId: siteID,
			}))
			if err != nil {
				return fmt.Errorf("failed to list site settings: %w", err)
			}
			for _, setting := range resp.Msg.Settings {
				fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%s\tsite:%s\n", setting.SettingId, setting.Key, setting.Value, setting.Editable, setting.Status, siteID)
			}
		} else {
			return fmt.Errorf("must specify one of --organization-id, --project-id, or --site-id")
		}

		return w.Flush()
	},
}

var editSettingCmd = &cobra.Command{
	Use:   "setting <setting-id>",
	Short: "Update a setting value",
	Long:  "Update a setting value for an organization, project, or site. Specify one of --organization-id, --project-id, or --site-id.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		value, _ := cmd.Flags().GetString("value")
		if !cmd.Flags().Changed("value") {
			return fmt.Errorf("--value is required")
		}
		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}
		orgID, projectID, siteID := settingScopeFlags(cmd)
		settingID := args[0]
		updateMask := &fieldmaskpb.FieldMask{Paths: []string{"value"}}

		if orgID != "" {
			resp, err := client.OrganizationSettingService.UpdateOrganizationSetting(cmd.Context(), connect.NewRequest(&libopsv1.UpdateOrganizationSettingRequest{
				OrganizationId: orgID,
				SettingId:      settingID,
				Value:          &value,
				UpdateMask:     updateMask,
			}))
			if err != nil {
				return fmt.Errorf("failed to update organization setting: %w", err)
			}
			fmt.Printf("Updated organization setting: %s (%s)\n", resp.Msg.Setting.SettingId, resp.Msg.Setting.Key)
			return nil
		}
		if projectID != "" {
			resp, err := client.ProjectSettingService.UpdateProjectSetting(cmd.Context(), connect.NewRequest(&libopsv1.UpdateProjectSettingRequest{
				ProjectId:  projectID,
				SettingId:  settingID,
				Value:      &value,
				UpdateMask: updateMask,
			}))
			if err != nil {
				return fmt.Errorf("failed to update project setting: %w", err)
			}
			fmt.Printf("Updated project setting: %s (%s)\n", resp.Msg.Setting.SettingId, resp.Msg.Setting.Key)
			return nil
		}
		if siteID != "" {
			resp, err := client.SiteSettingService.UpdateSiteSetting(cmd.Context(), connect.NewRequest(&libopsv1.UpdateSiteSettingRequest{
				SiteId:     siteID,
				SettingId:  settingID,
				Value:      &value,
				UpdateMask: updateMask,
			}))
			if err != nil {
				return fmt.Errorf("failed to update site setting: %w", err)
			}
			fmt.Printf("Updated site setting: %s (%s)\n", resp.Msg.Setting.SettingId, resp.Msg.Setting.Key)
			return nil
		}
		return fmt.Errorf("must specify one of --organization-id, --project-id, or --site-id")
	},
}

var deleteSettingCmd = &cobra.Command{
	Use:   "setting <setting-id>",
	Short: "Delete a setting",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		settingID := args[0]
		confirmed, err := confirmDeletion(cmd, "setting", settingID)
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
		orgID, projectID, siteID := settingScopeFlags(cmd)

		if orgID != "" {
			_, err = client.OrganizationSettingService.DeleteOrganizationSetting(cmd.Context(), connect.NewRequest(&libopsv1.DeleteOrganizationSettingRequest{
				OrganizationId: orgID,
				SettingId:      settingID,
			}))
		} else if projectID != "" {
			_, err = client.ProjectSettingService.DeleteProjectSetting(cmd.Context(), connect.NewRequest(&libopsv1.DeleteProjectSettingRequest{
				ProjectId: projectID,
				SettingId: settingID,
			}))
		} else if siteID != "" {
			_, err = client.SiteSettingService.DeleteSiteSetting(cmd.Context(), connect.NewRequest(&libopsv1.DeleteSiteSettingRequest{
				SiteId:    siteID,
				SettingId: settingID,
			}))
		} else {
			return fmt.Errorf("must specify one of --organization-id, --project-id, or --site-id")
		}
		if err != nil {
			return fmt.Errorf("failed to delete setting: %w", err)
		}

		fmt.Printf("Deleted setting: %s\n", settingID)
		return nil
	},
}

func settingScopeFlags(cmd *cobra.Command) (string, string, string) {
	orgID, _ := cmd.Flags().GetString("organization-id")
	projectID, _ := cmd.Flags().GetString("project-id")
	siteID, _ := cmd.Flags().GetString("site-id")
	return orgID, projectID, siteID
}

func addSettingScopeFlags(cmd *cobra.Command) {
	cmd.Flags().String("organization-id", "", "Organization ID")
	cmd.Flags().String("project-id", "", "Project ID")
	cmd.Flags().String("site-id", "", "Site ID")
	cmd.MarkFlagsOneRequired("organization-id", "project-id", "site-id")
	cmd.MarkFlagsMutuallyExclusive("organization-id", "project-id", "site-id")
}

func init() {
	createCmd.AddCommand(createSettingCmd)
	listCmd.AddCommand(listSettingsCmd)
	editCmd.AddCommand(editSettingCmd)
	deleteCmd.AddCommand(deleteSettingCmd)

	addSettingScopeFlags(createSettingCmd)
	createSettingCmd.Flags().String("key", "", "Setting key")
	createSettingCmd.Flags().String("value", "", "Setting value")
	createSettingCmd.Flags().Bool("editable", true, "Whether users may edit the setting")
	createSettingCmd.Flags().String("description", "", "Setting description")
	_ = createSettingCmd.MarkFlagRequired("key")

	addSettingScopeFlags(listSettingsCmd)

	addSettingScopeFlags(editSettingCmd)
	editSettingCmd.Flags().String("value", "", "Setting value")

	addSettingScopeFlags(deleteSettingCmd)
	deleteSettingCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}
