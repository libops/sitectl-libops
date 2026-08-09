package cmd

import (
	"context"
	"strings"
	"testing"

	commonv1 "github.com/libops/proto/libops/v1/common"
	"github.com/libops/sitectl-libops/pkg/api"
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

func TestDefaultPluginForSiteMatchesManagedRuntimeCatalog(t *testing.T) {
	tests := map[string]string{
		"archivesspace": "archivesspace",
		"drupal":        "drupal",
		"islandora":     "isle",
		"isle":          "isle",
		"ojs":           "ojs",
		"omeka-classic": "omeka-classic",
		"omeka-s":       "omeka-s",
		"wordpress":     "wp",
		"wp":            "wp",
		"unknown":       "",
	}

	for applicationType, expected := range tests {
		t.Run(applicationType, func(t *testing.T) {
			site := &commonv1.SiteConfig{ApplicationType: applicationType}
			if got := defaultPluginForSite(site); got != expected {
				t.Fatalf("defaultPluginForSite(%q) = %q, want %q", applicationType, got, expected)
			}
		})
	}
}

func TestManagedRuntimeSiteKeyIsStableAndCaseInsensitive(t *testing.T) {
	const siteID = "11111111-1111-1111-1111-111111111111"
	got := managedRuntimeSiteKey(siteID)
	if got != "site-bafde89c041e1756082b" {
		t.Fatalf("managedRuntimeSiteKey() = %q, want API and Cloud Compose runtime key", got)
	}
	if projectDir := managedRuntimeAppsRoot + "/" + got; projectDir != "/mnt/disks/data/libops/apps/site-bafde89c041e1756082b" {
		t.Fatalf("managed runtime project directory = %q", projectDir)
	}
	if upper := managedRuntimeSiteKey(strings.ToUpper(siteID)); upper != got {
		t.Fatalf("managedRuntimeSiteKey() differs by UUID case: %q != %q", upper, got)
	}
	if other := managedRuntimeSiteKey("44444444-4444-4444-8444-444444444444"); other == got {
		t.Fatalf("managedRuntimeSiteKey() collided for distinct site IDs: %q", got)
	}
	if empty := managedRuntimeSiteKey("  "); empty != "" {
		t.Fatalf("managedRuntimeSiteKey(empty) = %q, want empty", empty)
	}
}

func TestManagedDatabaseContractMatchesComposeTemplates(t *testing.T) {
	tests := map[string]managedDatabaseContract{
		"archivesspace": {service: "mariadb", user: "archivesspace", name: "archivesspace", passwordSecret: "ARCHIVESSPACE_DB_PASSWORD"},
		"drupal":        {service: "mariadb", user: "drupal", name: "drupal", passwordSecret: "DRUPAL_DEFAULT_DB_PASSWORD"},
		"islandora":     {service: "mariadb", user: "drupal_default", name: "drupal_default", passwordSecret: "DRUPAL_DEFAULT_DB_PASSWORD"},
		"isle":          {service: "mariadb", user: "drupal_default", name: "drupal_default", passwordSecret: "DRUPAL_DEFAULT_DB_PASSWORD"},
		"ojs":           {service: "mariadb", user: "ojs", name: "ojs", passwordSecret: "OJS_DB_PASSWORD"},
		"omeka-classic": {service: "mariadb", user: "omeka_classic", name: "omeka_classic", passwordSecret: "OMEKA_CLASSIC_DB_PASSWORD"},
		"omeka-s":       {service: "mariadb", user: "omeka_s", name: "omeka_s", passwordSecret: "OMEKA_S_DB_PASSWORD"},
		"wordpress":     {service: "mariadb", user: "wordpress", name: "wordpress", passwordSecret: "WORDPRESS_DB_PASSWORD"},
		"wp":            {service: "mariadb", user: "wordpress", name: "wordpress", passwordSecret: "WORDPRESS_DB_PASSWORD"},
	}

	for applicationType, expected := range tests {
		t.Run(applicationType, func(t *testing.T) {
			site := &commonv1.SiteConfig{ApplicationType: applicationType}
			got, ok := managedDatabaseContractForSite(site)
			if !ok || got != expected {
				t.Fatalf("managedDatabaseContractForSite(%q) = %#v, %t; want %#v, true", applicationType, got, ok, expected)
			}
		})
	}
	if _, ok := managedDatabaseContractForSite(&commonv1.SiteConfig{ApplicationType: "unknown"}); ok {
		t.Fatal("unknown application type returned a database contract")
	}
}

func TestManagedRuntimeEnvironmentMatchesControllerContract(t *testing.T) {
	if got := managedRuntimeEnvironment(&commonv1.SiteConfig{IsProduction: true}); got != "production" {
		t.Fatalf("managedRuntimeEnvironment(production) = %q", got)
	}
	if got := managedRuntimeEnvironment(&commonv1.SiteConfig{SiteName: "staging"}); got != "non-production" {
		t.Fatalf("managedRuntimeEnvironment(non-production) = %q", got)
	}
}

func TestResolveManagedRuntimeSSHUserPreservesExplicitOverride(t *testing.T) {
	got, err := resolveManagedRuntimeSSHUser(context.Background(), "not-a-url", " operator ")
	if err != nil || got != "operator" {
		t.Fatalf("resolveManagedRuntimeSSHUser(explicit) = %q, %v", got, err)
	}
}

func TestManagedRuntimeSSHAccountIDMatchesControllerUsernameContract(t *testing.T) {
	valid := &api.CurrentAccount{ID: "11111111-1111-4111-8111-111111111111"}
	if got, err := managedRuntimeSSHAccountID(valid); err != nil || got != valid.ID {
		t.Fatalf("managedRuntimeSSHAccountID(valid) = %q, %v", got, err)
	}

	for _, account := range []*api.CurrentAccount{
		nil,
		{},
		{ID: "not-a-uuid"},
		{ID: "00000000-0000-0000-0000-000000000000"},
		{ID: "11111111-1111-4111-8111-111111111111 "},
		{ID: "11111111-1111-4111-8111-11111111111A"},
	} {
		if _, err := managedRuntimeSSHAccountID(account); err == nil {
			t.Fatalf("managedRuntimeSSHAccountID(%#v) succeeded, want error", account)
		}
	}
}
