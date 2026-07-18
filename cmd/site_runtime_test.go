package cmd

import (
	"strings"
	"testing"

	commonv1 "github.com/libops/proto/libops/v1/common"
)

func TestPreferredSSHHostnameUsesExactAPIReturnedValue(t *testing.T) {
	domains := []*commonv1.DomainConfig{
		{
			Hostname:    "vanity.example.edu",
			Kind:        commonv1.DomainKind_DOMAIN_KIND_VANITY,
			SshHostname: "ssh-vanity.example.edu",
		},
		{
			Hostname:    "production.example.libops.site",
			Kind:        commonv1.DomainKind_DOMAIN_KIND_MANAGED,
			SshHostname: "ssh-production.example.libops.site.",
		},
	}

	if got := preferredSSHHostname(domains); got != "ssh-production.example.libops.site." {
		t.Fatalf("preferredSSHHostname() = %q, want exact managed SSH hostname", got)
	}
}

func TestPreferredSSHHostnameDoesNotDeriveFromHTTPHostname(t *testing.T) {
	domains := []*commonv1.DomainConfig{{
		Hostname:        "production.example.libops.site",
		ManagedHostname: "production.example.libops.site",
		Kind:            commonv1.DomainKind_DOMAIN_KIND_MANAGED,
	}}

	if got := preferredSSHHostname(domains); got != "" {
		t.Fatalf("preferredSSHHostname() = %q, want no derived hostname", got)
	}
	if err := missingSSHHostnameError(); err == nil || !strings.Contains(err.Error(), "--ssh-host") || !strings.Contains(err.Error(), "managed-domain provisioning") {
		t.Fatalf("missingSSHHostnameError() = %v, want actionable override and provisioning guidance", err)
	}
}

func TestPreferredSiteDomainUsesServerManagedHostname(t *testing.T) {
	domains := []*commonv1.DomainConfig{
		{Hostname: "vanity.example.edu", Kind: commonv1.DomainKind_DOMAIN_KIND_VANITY},
		{ManagedHostname: "production.example.libops.site", Kind: commonv1.DomainKind_DOMAIN_KIND_MANAGED},
	}

	if got := preferredSiteDomain(domains); got != "production.example.libops.site" {
		t.Fatalf("preferredSiteDomain() = %q, want server-managed hostname", got)
	}
}
