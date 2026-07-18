package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"

	libopsv1 "github.com/libops/proto/libops/v1"
	commonv1 "github.com/libops/proto/libops/v1/common"
	"github.com/libops/proto/libops/v1/libopsv1connect"
	libopsapi "github.com/libops/sitectl-libops/pkg/api"
	"github.com/libops/sitectl/pkg/format"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check resources",
}

var retryCmd = &cobra.Command{
	Use:   "retry",
	Short: "Retry failed operations",
}

var createDomainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Create a custom domain for a site",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newDomainServiceClient(cmd.Context(), cmd)
		if err != nil {
			return err
		}

		siteID, err := cmd.Flags().GetString("site-id")
		if err != nil {
			return err
		}
		hostname, err := cmd.Flags().GetString("domain")
		if err != nil {
			return err
		}

		return runCreateDomain(cmd.Context(), client, cmd.OutOrStdout(), siteID, hostname)
	},
}

var listDomainsCmd = &cobra.Command{
	Use:   "domains",
	Short: "List custom domains for a site",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newDomainServiceClient(cmd.Context(), cmd)
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

		resp, err := client.ListSiteDomains(cmd.Context(), connect.NewRequest(&libopsv1.ListSiteDomainsRequest{
			SiteId:   siteID,
			PageSize: 100,
		}))
		if err != nil {
			slog.Error("Failed to list domains", "site_id", siteID, "err", err)
			return err
		}

		headers := []string{"ID", "HOSTNAME", "PROVISIONING", "ROUTE", "READY", "SSH", "FAILURE", "UPDATED"}
		rows := make([][]string, 0, len(resp.Msg.GetDomains()))
		data := make([]interface{}, 0, len(resp.Msg.GetDomains()))
		for _, domain := range resp.Msg.GetDomains() {
			provisioning := domainProvisioningStateLabel(domain.GetProvisioningState())
			route := domainRouteStateLabel(domain.GetRouteState())
			updated := protobufTimeLabel(domain.GetUpdatedAt())
			rows = append(rows, []string{
				domain.GetDomainId(),
				domain.GetHostname(),
				provisioning,
				route,
				yesNo(domain.GetRouteReady()),
				domain.GetSshHostname(),
				domain.GetFailureReason(),
				updated,
			})
			data = append(data, map[string]interface{}{
				"DomainId":          domain.GetDomainId(),
				"SiteId":            domain.GetSiteId(),
				"Hostname":          domain.GetHostname(),
				"ProvisioningState": provisioning,
				"RouteState":        route,
				"RouteReady":        domain.GetRouteReady(),
				"ManagedHostname":   domain.GetManagedHostname(),
				"SshHostname":       domain.GetSshHostname(),
				"FailureReason":     domain.GetFailureReason(),
				"Retryable":         domain.GetRetryable(),
				"CreatedAt":         protobufTimeLabel(domain.GetCreatedAt()),
				"UpdatedAt":         updated,
				"VerifiedAt":        protobufTimeLabel(domain.GetVerifiedAt()),
				"RouteReadyAt":      protobufTimeLabel(domain.GetRouteReadyAt()),
				"ActivatedAt":       protobufTimeLabel(domain.GetActivatedAt()),
				"DeleteRequestedAt": protobufTimeLabel(domain.GetDeleteRequestedAt()),
			})
		}

		return formatter.Print(data, headers, rows)
	},
}

var getDomainCmd = &cobra.Command{
	Use:   "domain <domain-id>",
	Short: "Get a custom domain and its current DNS instructions",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newDomainServiceClient(cmd.Context(), cmd)
		if err != nil {
			return err
		}
		siteID, err := cmd.Flags().GetString("site-id")
		if err != nil {
			return err
		}

		return runGetDomain(cmd.Context(), client, cmd.OutOrStdout(), siteID, args[0])
	},
}

var checkDomainCmd = &cobra.Command{
	Use:   "domain <domain-id>",
	Short: "Check observed DNS state and wake domain reconciliation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newDomainServiceClient(cmd.Context(), cmd)
		if err != nil {
			return err
		}
		siteID, err := cmd.Flags().GetString("site-id")
		if err != nil {
			return err
		}

		return runCheckDomain(cmd.Context(), client, cmd.OutOrStdout(), siteID, args[0])
	},
}

var retryDomainCmd = &cobra.Command{
	Use:   "domain <domain-id>",
	Short: "Retry a failed custom-domain reconciliation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newDomainServiceClient(cmd.Context(), cmd)
		if err != nil {
			return err
		}
		siteID, err := cmd.Flags().GetString("site-id")
		if err != nil {
			return err
		}

		return runRetryDomain(cmd.Context(), client, cmd.OutOrStdout(), siteID, args[0])
	},
}

var deleteDomainCmd = &cobra.Command{
	Use:   "domain <domain-id>",
	Short: "Start asynchronous cleanup of a custom domain",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		domainID := args[0]
		confirmed, err := confirmDeletion(cmd, "domain", domainID)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.OutOrStdout(), "Deletion cancelled.")
			return nil
		}

		client, err := newDomainServiceClient(cmd.Context(), cmd)
		if err != nil {
			return err
		}
		siteID, err := cmd.Flags().GetString("site-id")
		if err != nil {
			return err
		}

		return runDeleteDomain(cmd.Context(), client, cmd.OutOrStdout(), siteID, domainID)
	},
}

func runCreateDomain(ctx context.Context, client libopsv1connect.DomainServiceClient, w io.Writer, siteID, hostname string) error {
	resp, err := client.CreateSiteDomain(ctx, connect.NewRequest(&libopsv1.CreateSiteDomainRequest{
		SiteId:   siteID,
		Hostname: hostname,
	}))
	if err != nil {
		slog.Error("Failed to create domain", "site_id", siteID, "hostname", hostname, "err", err)
		return err
	}
	return writeDomainDetails(w, "Created pending domain", resp.Msg.GetDomain(), resp.Msg.GetOwnershipProof())
}

func runGetDomain(ctx context.Context, client libopsv1connect.DomainServiceClient, w io.Writer, siteID, domainID string) error {
	resp, err := client.GetSiteDomain(ctx, connect.NewRequest(&libopsv1.GetSiteDomainRequest{
		SiteId:   siteID,
		DomainId: domainID,
	}))
	if err != nil {
		slog.Error("Failed to get domain", "site_id", siteID, "domain_id", domainID, "err", err)
		return err
	}
	return writeDomainDetails(w, "Domain", resp.Msg.GetDomain(), nil)
}

func runCheckDomain(ctx context.Context, client libopsv1connect.DomainServiceClient, w io.Writer, siteID, domainID string) error {
	resp, err := client.CheckSiteDomain(ctx, connect.NewRequest(&libopsv1.CheckSiteDomainRequest{
		SiteId:   siteID,
		DomainId: domainID,
	}))
	if err != nil {
		slog.Error("Failed to check domain", "site_id", siteID, "domain_id", domainID, "err", err)
		return err
	}
	return writeDomainDetails(w, "Checked domain", resp.Msg.GetDomain(), nil)
}

func runRetryDomain(ctx context.Context, client libopsv1connect.DomainServiceClient, w io.Writer, siteID, domainID string) error {
	resp, err := client.RetrySiteDomainProvisioning(ctx, connect.NewRequest(&libopsv1.RetrySiteDomainProvisioningRequest{
		SiteId:   siteID,
		DomainId: domainID,
	}))
	if err != nil {
		slog.Error("Failed to retry domain", "site_id", siteID, "domain_id", domainID, "err", err)
		return err
	}
	return writeDomainDetails(w, "Retried domain", resp.Msg.GetDomain(), resp.Msg.GetOwnershipProof())
}

func runDeleteDomain(ctx context.Context, client libopsv1connect.DomainServiceClient, w io.Writer, siteID, domainID string) error {
	_, err := client.DeleteSiteDomain(ctx, connect.NewRequest(&libopsv1.DeleteSiteDomainRequest{
		SiteId:   siteID,
		DomainId: domainID,
	}))
	if err != nil {
		slog.Error("Failed to delete domain", "site_id", siteID, "domain_id", domainID, "err", err)
		return err
	}
	fmt.Fprintf(w, "Started domain cleanup: %s\n", domainID)
	return nil
}

func newDomainServiceClient(ctx context.Context, cmd *cobra.Command) (libopsv1connect.DomainServiceClient, error) {
	apiBaseURL, err := cmd.Flags().GetString("api-url")
	if err != nil {
		return nil, err
	}
	client, err := libopsapi.NewLibopsAPIClient(ctx, apiBaseURL)
	if err != nil {
		return nil, err
	}
	return client.DomainService, nil
}

func writeDomainDetails(w io.Writer, headline string, domain *commonv1.DomainConfig, additionalInstruction *commonv1.DnsRecordInstruction) error {
	if domain == nil {
		return fmt.Errorf("LibOps API returned an empty domain response")
	}

	fmt.Fprintf(w, "\u2713 %s\n", headline)
	fmt.Fprintf(w, "  Domain ID: %s\n", domain.GetDomainId())
	fmt.Fprintf(w, "  Site ID: %s\n", domain.GetSiteId())
	fmt.Fprintf(w, "  Hostname: %s\n", domain.GetHostname())
	fmt.Fprintf(w, "  Provisioning: %s\n", domainProvisioningStateLabel(domain.GetProvisioningState()))
	fmt.Fprintf(w, "  Route: %s\n", domainRouteStateLabel(domain.GetRouteState()))
	fmt.Fprintf(w, "  Route ready: %s\n", yesNo(domain.GetRouteReady()))
	if value := strings.TrimSpace(domain.GetManagedHostname()); value != "" {
		fmt.Fprintf(w, "  Managed hostname: %s\n", value)
	}
	if value := strings.TrimSpace(domain.GetSshHostname()); value != "" {
		fmt.Fprintf(w, "  SSH hostname: %s\n", value)
	}
	if value := strings.TrimSpace(domain.GetFailureReason()); value != "" {
		fmt.Fprintf(w, "  Failure reason: %s\n", value)
	}
	if domain.GetRetryable() {
		fmt.Fprintln(w, "  Retryable: yes")
	}
	writeDomainTimes(w, domain)

	instructions := uniqueDNSInstructions(additionalInstruction, domain.GetDnsInstructions())
	if len(instructions) == 0 {
		fmt.Fprintln(w, "  DNS instructions: none currently available")
		return nil
	}

	fmt.Fprintln(w, "  DNS instructions:")
	for _, instruction := range instructions {
		fmt.Fprintf(w, "    %s\n", dnsRecordPurposeLabel(instruction.GetPurpose()))
		fmt.Fprintf(w, "      Name: %s\n", instruction.GetName())
		fmt.Fprintf(w, "      Type: %s\n", strings.ToUpper(instruction.GetType()))
		fmt.Fprintf(w, "      Value: %s\n", instruction.GetData())
		if instruction.GetTtl() > 0 {
			fmt.Fprintf(w, "      TTL: %d\n", instruction.GetTtl())
		}
		fmt.Fprintf(w, "      Retain: %s\n", yesNo(instruction.GetRetain()))
	}
	return nil
}

func writeDomainTimes(w io.Writer, domain *commonv1.DomainConfig) {
	fields := []struct {
		label string
		value string
	}{
		{label: "Created", value: protobufTimeLabel(domain.GetCreatedAt())},
		{label: "Updated", value: protobufTimeLabel(domain.GetUpdatedAt())},
		{label: "Verified", value: protobufTimeLabel(domain.GetVerifiedAt())},
		{label: "Route ready at", value: protobufTimeLabel(domain.GetRouteReadyAt())},
		{label: "Activated", value: protobufTimeLabel(domain.GetActivatedAt())},
		{label: "Delete requested", value: protobufTimeLabel(domain.GetDeleteRequestedAt())},
	}
	for _, field := range fields {
		if field.value != "" {
			fmt.Fprintf(w, "  %s: %s\n", field.label, field.value)
		}
	}
}

func uniqueDNSInstructions(additional *commonv1.DnsRecordInstruction, instructions []*commonv1.DnsRecordInstruction) []*commonv1.DnsRecordInstruction {
	result := make([]*commonv1.DnsRecordInstruction, 0, len(instructions)+1)
	seen := make(map[string]struct{}, len(instructions)+1)
	appendInstruction := func(instruction *commonv1.DnsRecordInstruction) {
		if instruction == nil {
			return
		}
		key := fmt.Sprintf("%d\x00%s\x00%s\x00%s", instruction.GetPurpose(), instruction.GetName(), instruction.GetType(), instruction.GetData())
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, instruction)
	}
	appendInstruction(additional)
	for _, instruction := range instructions {
		appendInstruction(instruction)
	}
	return result
}

func domainProvisioningStateLabel(state commonv1.DomainProvisioningState) string {
	return enumLabel(state.String(), "DOMAIN_PROVISIONING_STATE_")
}

func domainRouteStateLabel(state commonv1.DomainRouteState) string {
	return enumLabel(state.String(), "DOMAIN_ROUTE_STATE_")
}

func dnsRecordPurposeLabel(purpose commonv1.DnsRecordPurpose) string {
	return enumLabel(purpose.String(), "DNS_RECORD_PURPOSE_")
}

func enumLabel(value, prefix string) string {
	value = strings.TrimPrefix(value, prefix)
	value = strings.ToLower(strings.ReplaceAll(value, "_", " "))
	if value == "" {
		return "unspecified"
	}
	return value
}

func protobufTimeLabel(value *timestamppb.Timestamp) string {
	if value == nil || !value.IsValid() {
		return ""
	}
	return value.AsTime().UTC().Format(time.RFC3339)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func init() {
	createCmd.AddCommand(createDomainCmd)
	listCmd.AddCommand(listDomainsCmd)
	getCmd.AddCommand(getDomainCmd)
	checkCmd.AddCommand(checkDomainCmd)
	retryCmd.AddCommand(retryDomainCmd)
	deleteCmd.AddCommand(deleteDomainCmd)

	createDomainCmd.Flags().String("site-id", "", "Site ID")
	createDomainCmd.Flags().String("domain", "", "Domain hostname")
	_ = createDomainCmd.MarkFlagRequired("site-id")
	_ = createDomainCmd.MarkFlagRequired("domain")

	listDomainsCmd.Flags().String("site-id", "", "Site ID")
	_ = listDomainsCmd.MarkFlagRequired("site-id")

	getDomainCmd.Flags().String("site-id", "", "Site ID")
	_ = getDomainCmd.MarkFlagRequired("site-id")

	checkDomainCmd.Flags().String("site-id", "", "Site ID")
	_ = checkDomainCmd.MarkFlagRequired("site-id")

	retryDomainCmd.Flags().String("site-id", "", "Site ID")
	_ = retryDomainCmd.MarkFlagRequired("site-id")

	deleteDomainCmd.Flags().String("site-id", "", "Site ID")
	deleteDomainCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	_ = deleteDomainCmd.MarkFlagRequired("site-id")
}
