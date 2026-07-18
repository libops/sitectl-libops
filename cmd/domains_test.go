package cmd

import (
	"bytes"
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"connectrpc.com/connect"

	libopsv1 "github.com/libops/proto/libops/v1"
	commonv1 "github.com/libops/proto/libops/v1/common"
	"github.com/libops/proto/libops/v1/libopsv1connect"
	"github.com/spf13/pflag"
	"google.golang.org/protobuf/types/known/emptypb"
)

type recordingDomainServiceClient struct {
	libopsv1connect.DomainServiceClient
	createRequest *libopsv1.CreateSiteDomainRequest
	getRequest    *libopsv1.GetSiteDomainRequest
	checkRequest  *libopsv1.CheckSiteDomainRequest
	retryRequest  *libopsv1.RetrySiteDomainProvisioningRequest
	deleteRequest *libopsv1.DeleteSiteDomainRequest
	domain        *commonv1.DomainConfig
	proof         *commonv1.DnsRecordInstruction
}

func (c *recordingDomainServiceClient) CreateSiteDomain(_ context.Context, req *connect.Request[libopsv1.CreateSiteDomainRequest]) (*connect.Response[libopsv1.CreateSiteDomainResponse], error) {
	c.createRequest = req.Msg
	return connect.NewResponse(&libopsv1.CreateSiteDomainResponse{Domain: c.domain, OwnershipProof: c.proof}), nil
}

func (c *recordingDomainServiceClient) GetSiteDomain(_ context.Context, req *connect.Request[libopsv1.GetSiteDomainRequest]) (*connect.Response[libopsv1.GetSiteDomainResponse], error) {
	c.getRequest = req.Msg
	return connect.NewResponse(&libopsv1.GetSiteDomainResponse{Domain: c.domain}), nil
}

func (c *recordingDomainServiceClient) CheckSiteDomain(_ context.Context, req *connect.Request[libopsv1.CheckSiteDomainRequest]) (*connect.Response[libopsv1.CheckSiteDomainResponse], error) {
	c.checkRequest = req.Msg
	return connect.NewResponse(&libopsv1.CheckSiteDomainResponse{Domain: c.domain}), nil
}

func (c *recordingDomainServiceClient) RetrySiteDomainProvisioning(_ context.Context, req *connect.Request[libopsv1.RetrySiteDomainProvisioningRequest]) (*connect.Response[libopsv1.RetrySiteDomainProvisioningResponse], error) {
	c.retryRequest = req.Msg
	return connect.NewResponse(&libopsv1.RetrySiteDomainProvisioningResponse{Domain: c.domain, OwnershipProof: c.proof}), nil
}

func (c *recordingDomainServiceClient) DeleteSiteDomain(_ context.Context, req *connect.Request[libopsv1.DeleteSiteDomainRequest]) (*connect.Response[emptypb.Empty], error) {
	c.deleteRequest = req.Msg
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func TestRunCreateDomainUsesPendingServerContract(t *testing.T) {
	proof := &commonv1.DnsRecordInstruction{
		Name:    "_libops-proof.journals.example.edu",
		Type:    "TXT",
		Data:    "proof-value",
		Ttl:     300,
		Purpose: commonv1.DnsRecordPurpose_DNS_RECORD_PURPOSE_OWNERSHIP_PROOF,
	}
	client := &recordingDomainServiceClient{
		domain: &commonv1.DomainConfig{
			DomainId:          "domain-123",
			SiteId:            "site-456",
			Hostname:          "journals.example.edu",
			ProvisioningState: commonv1.DomainProvisioningState_DOMAIN_PROVISIONING_STATE_AWAITING_OWNERSHIP_PROOF,
			RouteState:        commonv1.DomainRouteState_DOMAIN_ROUTE_STATE_PENDING,
			DnsInstructions:   []*commonv1.DnsRecordInstruction{proof},
		},
		proof: proof,
	}
	var output bytes.Buffer

	if err := runCreateDomain(context.Background(), client, &output, "site-456", "journals.example.edu"); err != nil {
		t.Fatalf("runCreateDomain() error = %v", err)
	}
	if client.createRequest.GetSiteId() != "site-456" || client.createRequest.GetHostname() != "journals.example.edu" {
		t.Fatalf("create request = %#v", client.createRequest)
	}
	for _, want := range []string{
		"Created pending domain",
		"Domain ID: domain-123",
		"Provisioning: awaiting ownership proof",
		"Route: pending",
		"DNS instructions:",
		"ownership proof",
		"Name: _libops-proof.journals.example.edu",
		"Type: TXT",
		"Value: proof-value",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
	if got := strings.Count(output.String(), "Value: proof-value"); got != 1 {
		t.Fatalf("ownership proof printed %d times, want 1:\n%s", got, output.String())
	}
	for _, forbidden := range []string{"status", "edge action", "origin", "provider", "tier", "sample rate"} {
		if strings.Contains(strings.ToLower(output.String()), forbidden) {
			t.Errorf("output exposes forbidden field %q:\n%s", forbidden, output.String())
		}
	}
}

func TestStableDomainOperationsUseDomainID(t *testing.T) {
	client := &recordingDomainServiceClient{
		domain: &commonv1.DomainConfig{
			DomainId:          "domain-123",
			SiteId:            "site-456",
			Hostname:          "journals.example.edu",
			ProvisioningState: commonv1.DomainProvisioningState_DOMAIN_PROVISIONING_STATE_FAILED,
			RouteState:        commonv1.DomainRouteState_DOMAIN_ROUTE_STATE_BLOCKED,
			Retryable:         true,
		},
	}
	var output bytes.Buffer
	ctx := context.Background()

	if err := runGetDomain(ctx, client, &output, "site-456", "domain-123"); err != nil {
		t.Fatalf("runGetDomain() error = %v", err)
	}
	if err := runCheckDomain(ctx, client, &output, "site-456", "domain-123"); err != nil {
		t.Fatalf("runCheckDomain() error = %v", err)
	}
	if err := runRetryDomain(ctx, client, &output, "site-456", "domain-123"); err != nil {
		t.Fatalf("runRetryDomain() error = %v", err)
	}
	if err := runDeleteDomain(ctx, client, &output, "site-456", "domain-123"); err != nil {
		t.Fatalf("runDeleteDomain() error = %v", err)
	}

	requests := []interface {
		GetSiteId() string
		GetDomainId() string
	}{client.getRequest, client.checkRequest, client.retryRequest, client.deleteRequest}
	for _, request := range requests {
		if request.GetSiteId() != "site-456" || request.GetDomainId() != "domain-123" {
			t.Errorf("request site/domain IDs = %q/%q", request.GetSiteId(), request.GetDomainId())
		}
	}
	if !strings.Contains(output.String(), "Started domain cleanup: domain-123") {
		t.Fatalf("delete output missing stable domain ID:\n%s", output.String())
	}
}

func TestDomainCommandFlagsMatchServerOwnedContract(t *testing.T) {
	tests := []struct {
		name string
		cmd  interface{ LocalNonPersistentFlags() *pflag.FlagSet }
		want []string
	}{
		{name: "create", cmd: createDomainCmd, want: []string{"domain", "site-id"}},
		{name: "list", cmd: listDomainsCmd, want: []string{"site-id"}},
		{name: "get", cmd: getDomainCmd, want: []string{"site-id"}},
		{name: "check", cmd: checkDomainCmd, want: []string{"site-id"}},
		{name: "retry", cmd: retryDomainCmd, want: []string{"site-id"}},
		{name: "delete", cmd: deleteDomainCmd, want: []string{"site-id", "yes"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			tt.cmd.LocalNonPersistentFlags().VisitAll(func(flag *pflag.Flag) {
				got = append(got, flag.Name)
			})
			sort.Strings(got)
			sort.Strings(tt.want)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("local flags = %v, want %v", got, tt.want)
			}
		})
	}
}
