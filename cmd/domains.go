package cmd

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"connectrpc.com/connect"

	libopsv1 "github.com/libops/proto/libops/v1"
	commonv1 "github.com/libops/proto/libops/v1/common"
	libopsapi "github.com/libops/sitectl-libops/pkg/api"
	"github.com/libops/sitectl/pkg/format"
	"github.com/spf13/cobra"
)

var createDomainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Create a custom domain for a site",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		client, err := libopsapi.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		siteID, err := cmd.Flags().GetString("site-id")
		if err != nil {
			return err
		}
		domainName, err := cmd.Flags().GetString("domain")
		if err != nil {
			return err
		}
		tier, err := cmd.Flags().GetString("tier")
		if err != nil {
			return err
		}
		statusValue, err := cmd.Flags().GetString("status")
		if err != nil {
			return err
		}
		status, err := domainStatus(statusValue)
		if err != nil {
			return err
		}
		edgeActionValue, err := cmd.Flags().GetString("edge-action")
		if err != nil {
			return err
		}
		edgeAction, err := domainEdgeAction(edgeActionValue)
		if err != nil {
			return err
		}
		sampleRateValue, err := cmd.Flags().GetString("success-log-sample-rate")
		if err != nil {
			return err
		}
		sampleRate := 0.0
		if sampleRateValue != "" {
			sampleRate, err = strconv.ParseFloat(sampleRateValue, 64)
			if err != nil {
				return fmt.Errorf("invalid success-log-sample-rate: %w", err)
			}
		}

		resp, err := client.DomainService.CreateSiteDomain(cmd.Context(), connect.NewRequest(&libopsv1.CreateSiteDomainRequest{
			SiteId: siteID,
			Domain: &commonv1.DomainConfig{
				Domain:               domainName,
				SiteId:               siteID,
				Tier:                 tier,
				Status:               status,
				EdgeAction:           edgeAction,
				SuccessLogSampleRate: sampleRate,
			},
		}))
		if err != nil {
			slog.Error("Failed to create domain", "site_id", siteID, "domain", domainName, "err", err)
			return err
		}

		fmt.Printf("✓ Created domain\n")
		fmt.Printf("  Domain: %s\n", resp.Msg.Domain.Domain)
		fmt.Printf("  Site ID: %s\n", resp.Msg.Domain.SiteId)
		fmt.Printf("  Status: %s\n", domainStatusLabel(resp.Msg.Domain.Status))
		fmt.Printf("  Edge Action: %s\n", domainEdgeActionLabel(resp.Msg.Domain.EdgeAction))
		fmt.Printf("  Target: %s\n", resp.Msg.Domain.CloudRunHostname)
		return nil
	},
}

var listDomainsCmd = &cobra.Command{
	Use:   "domains",
	Short: "List custom domains for a site",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		client, err := libopsapi.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		siteID, err := cmd.Flags().GetString("site-id")
		if err != nil {
			return err
		}
		formatStr, err := cmd.Flags().GetString("format")
		if err != nil {
			return err
		}
		formatter, err := format.NewFormatter(formatStr)
		if err != nil {
			return fmt.Errorf("invalid format: %w", err)
		}

		resp, err := client.DomainService.ListSiteDomains(cmd.Context(), connect.NewRequest(&libopsv1.ListSiteDomainsRequest{
			SiteId:   siteID,
			PageSize: 100,
		}))
		if err != nil {
			slog.Error("Failed to list domains", "site_id", siteID, "err", err)
			return err
		}

		headers := []string{"DOMAIN", "STATUS", "EDGE", "TIER", "TARGET"}
		rows := make([][]string, 0, len(resp.Msg.Domains))
		data := make([]interface{}, 0, len(resp.Msg.Domains))
		for _, domain := range resp.Msg.Domains {
			status := domainStatusLabel(domain.Status)
			edgeAction := domainEdgeActionLabel(domain.EdgeAction)
			rows = append(rows, []string{
				domain.Domain,
				status,
				edgeAction,
				domain.Tier,
				domain.CloudRunHostname,
			})
			data = append(data, map[string]interface{}{
				"Domain":           domain.Domain,
				"SiteId":           domain.SiteId,
				"Status":           status,
				"EdgeAction":       edgeAction,
				"Tier":             domain.Tier,
				"CloudRunHostname": domain.CloudRunHostname,
			})
		}

		return formatter.Print(data, headers, rows)
	},
}

var deleteDomainCmd = &cobra.Command{
	Use:   "domain <domain>",
	Short: "Delete a custom domain from a site",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		domainName := args[0]
		confirmed, err := confirmDeletion(cmd, "domain", domainName)
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
		client, err := libopsapi.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}
		siteID, err := cmd.Flags().GetString("site-id")
		if err != nil {
			return err
		}

		_, err = client.DomainService.DeleteSiteDomain(cmd.Context(), connect.NewRequest(&libopsv1.DeleteSiteDomainRequest{
			SiteId: siteID,
			Domain: domainName,
		}))
		if err != nil {
			slog.Error("Failed to delete domain", "site_id", siteID, "domain", domainName, "err", err)
			return err
		}

		fmt.Printf("✓ Deleted domain: %s\n", domainName)
		return nil
	},
}

func domainStatus(value string) (commonv1.Status, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "active":
		return commonv1.Status_STATUS_ACTIVE, nil
	case "pending", "provisioning":
		return commonv1.Status_STATUS_PROVISIONING, nil
	case "suspended":
		return commonv1.Status_STATUS_SUSPENDED, nil
	case "deleted", "blocked":
		return commonv1.Status_STATUS_DELETED, nil
	default:
		return commonv1.Status_STATUS_UNSPECIFIED, fmt.Errorf("invalid status %q", value)
	}
}

func domainStatusLabel(status commonv1.Status) string {
	switch status {
	case commonv1.Status_STATUS_ACTIVE:
		return "active"
	case commonv1.Status_STATUS_PROVISIONING:
		return "pending"
	case commonv1.Status_STATUS_SUSPENDED:
		return "suspended"
	case commonv1.Status_STATUS_DELETED:
		return "blocked"
	default:
		return "unspecified"
	}
}

func domainEdgeAction(value string) (commonv1.DomainEdgeAction, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "allow":
		return commonv1.DomainEdgeAction_DOMAIN_EDGE_ACTION_ALLOW, nil
	case "challenge":
		return commonv1.DomainEdgeAction_DOMAIN_EDGE_ACTION_CHALLENGE, nil
	case "block":
		return commonv1.DomainEdgeAction_DOMAIN_EDGE_ACTION_BLOCK, nil
	default:
		return commonv1.DomainEdgeAction_DOMAIN_EDGE_ACTION_UNSPECIFIED, fmt.Errorf("invalid edge-action %q", value)
	}
}

func domainEdgeActionLabel(action commonv1.DomainEdgeAction) string {
	switch action {
	case commonv1.DomainEdgeAction_DOMAIN_EDGE_ACTION_ALLOW:
		return "allow"
	case commonv1.DomainEdgeAction_DOMAIN_EDGE_ACTION_CHALLENGE:
		return "challenge"
	case commonv1.DomainEdgeAction_DOMAIN_EDGE_ACTION_BLOCK:
		return "block"
	default:
		return "unspecified"
	}
}

func init() {
	createCmd.AddCommand(createDomainCmd)
	listCmd.AddCommand(listDomainsCmd)
	deleteCmd.AddCommand(deleteDomainCmd)

	createDomainCmd.Flags().String("site-id", "", "Site ID")
	createDomainCmd.Flags().String("domain", "", "Domain hostname")
	createDomainCmd.Flags().String("tier", "standard", "Domain service tier")
	createDomainCmd.Flags().String("status", "active", "Domain status: active, pending, suspended, or blocked")
	createDomainCmd.Flags().String("edge-action", "allow", "Edge action: allow, challenge, or block")
	createDomainCmd.Flags().String("success-log-sample-rate", "", "Success log sample rate between 0 and 1")
	_ = createDomainCmd.MarkFlagRequired("site-id")
	_ = createDomainCmd.MarkFlagRequired("domain")

	listDomainsCmd.Flags().String("site-id", "", "Site ID")
	_ = listDomainsCmd.MarkFlagRequired("site-id")

	deleteDomainCmd.Flags().String("site-id", "", "Site ID")
	deleteDomainCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	_ = deleteDomainCmd.MarkFlagRequired("site-id")
}
