package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	libopsv1 "github.com/libops/proto/libops/v1"
	commonv1 "github.com/libops/proto/libops/v1/common"
	"github.com/libops/sitectl-libops/pkg/api"
	sitectlconfig "github.com/libops/sitectl/pkg/config"
	"github.com/spf13/cobra"
)

type siteEnvironment struct {
	site    *commonv1.SiteConfig
	domains []*commonv1.DomainConfig
}

const managedRuntimeAppsRoot = "/mnt/disks/data/libops/apps"

var pingCmd = &cobra.Command{
	Use:   "ping <site-id-or-url>",
	Short: "Ping a site URL to wake it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := strings.TrimSpace(args[0])
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		timeout, _ := cmd.Flags().GetDuration("timeout")
		if timeout <= 0 {
			timeout = 30 * time.Second
		}

		pingURL := target
		if !looksLikeURL(target) {
			client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
			if err != nil {
				return err
			}
			env, err := loadSiteEnvironment(cmd.Context(), client, target)
			if err != nil {
				return err
			}
			domain := preferredSiteDomain(env.domains)
			if domain == "" {
				return fmt.Errorf("site has no domains; pass a URL instead")
			}
			pingURL = "https://" + domain
		}
		if !strings.HasPrefix(pingURL, "http://") && !strings.HasPrefix(pingURL, "https://") {
			pingURL = "https://" + pingURL
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pingURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "sitectl-libops/1")
		start := time.Now()
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return fmt.Errorf("ping failed: %w", err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)

		fmt.Printf("Pinged %s: %s in %s\n", pingURL, resp.Status, time.Since(start).Round(time.Millisecond))
		return nil
	},
}

var sshCmd = &cobra.Command{
	Use:   "ssh <site-id> [-- ssh-args...]",
	Short: "SSH into a site environment",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		sshHost, _ := cmd.Flags().GetString("ssh-host")
		sshUser, _ := cmd.Flags().GetString("ssh-user")
		sshKey, _ := cmd.Flags().GetString("ssh-key")
		sshPort, _ := cmd.Flags().GetUint("ssh-port")
		sshUser, err = resolveManagedRuntimeSSHUser(cmd.Context(), apiBaseURL, sshUser)
		if err != nil {
			return err
		}
		if sshPort == 0 {
			sshPort = 22
		}

		siteID := args[0]
		extraArgs := args[1:]
		if sshHost == "" {
			client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
			if err != nil {
				return err
			}
			env, err := loadSiteEnvironment(cmd.Context(), client, siteID)
			if err != nil {
				return err
			}
			sshHost = preferredSSHHostname(env.domains)
			if sshHost == "" {
				return missingSSHHostnameError()
			}
		}

		sshArgs := []string{"-p", fmt.Sprintf("%d", sshPort)}
		if sshKey != "" {
			sshArgs = append(sshArgs, "-i", sshKey)
		}
		sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", sshUser, sshHost))
		sshArgs = append(sshArgs, extraArgs...)

		ssh := exec.CommandContext(cmd.Context(), "ssh", sshArgs...) // #nosec G204 -- command is fixed and user-controlled values are passed as argv.
		ssh.Stdin = os.Stdin
		ssh.Stdout = os.Stdout
		ssh.Stderr = os.Stderr
		return ssh.Run()
	},
}

var checkoutCmd = &cobra.Command{
	Use:   "checkout <site-id> [directory]",
	Short: "Checkout a site environment repository locally",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}
		env, err := loadSiteEnvironment(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		repo := normalizeGitRemote(env.site.GetGithubRepository())
		if repo == "" {
			return fmt.Errorf("site has no github_repository")
		}

		targetDir := ""
		if len(args) == 2 {
			targetDir = args[1]
		} else {
			targetDir = defaultCheckoutDir(repo, env.site.GetSiteName())
		}

		clone := exec.CommandContext(cmd.Context(), "git", "clone", repo, targetDir) // #nosec G204 -- command is fixed and repo/dir are argv.
		clone.Stdin = os.Stdin
		clone.Stdout = os.Stdout
		clone.Stderr = os.Stderr
		if err := clone.Run(); err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}

		ref := normalizeGitRef(env.site.GetGithubRef())
		if ref != "" {
			checkout := exec.CommandContext(cmd.Context(), "git", "-C", targetDir, "checkout", ref) // #nosec G204 -- command is fixed and ref/dir are argv.
			checkout.Stdin = os.Stdin
			checkout.Stdout = os.Stdout
			checkout.Stderr = os.Stderr
			if err := checkout.Run(); err != nil {
				return fmt.Errorf("git checkout %s failed: %w", ref, err)
			}
		}

		updateContext, _ := cmd.Flags().GetBool("update-context")
		if updateContext {
			if err := saveSiteContext(cmd, env); err != nil {
				return err
			}
		}
		return nil
	},
}

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage sitectl contexts from LibOps site environments",
}

var contextUpdateCmd = &cobra.Command{
	Use:   "update <site-id>",
	Short: "Update a sitectl context from a site environment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}
		env, err := loadSiteEnvironment(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		return saveSiteContext(cmd, env)
	},
}

func loadSiteEnvironment(ctx context.Context, client *api.LibopsAPIClient, siteID string) (*siteEnvironment, error) {
	siteResp, err := client.SiteService.GetSite(ctx, connect.NewRequest(&libopsv1.GetSiteRequest{SiteId: siteID}))
	if err != nil {
		return nil, fmt.Errorf("failed to get site: %w", err)
	}
	site := siteResp.Msg.GetSite()
	if site == nil {
		return nil, fmt.Errorf("site not found")
	}
	domainResp, err := client.DomainService.ListSiteDomains(ctx, connect.NewRequest(&libopsv1.ListSiteDomainsRequest{
		SiteId: site.GetSiteId(),
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to list site domains: %w", err)
	}
	return &siteEnvironment{
		site:    site,
		domains: domainResp.Msg.GetDomains(),
	}, nil
}

func saveSiteContext(cmd *cobra.Command, env *siteEnvironment) error {
	contextName, _ := cmd.Flags().GetString("context-name")
	projectDir, _ := cmd.Flags().GetString("project-dir")
	pluginName, _ := cmd.Flags().GetString("plugin")
	sshHost, _ := cmd.Flags().GetString("ssh-host")
	sshUser, _ := cmd.Flags().GetString("ssh-user")
	sshKey, _ := cmd.Flags().GetString("ssh-key")
	sshPort, _ := cmd.Flags().GetUint("ssh-port")
	setDefault, _ := cmd.Flags().GetBool("default")

	runtimeSiteKey := managedRuntimeSiteKey(env.site.GetSiteId())
	if runtimeSiteKey == "" {
		return fmt.Errorf("site ID is required to resolve the managed runtime Compose project")
	}
	if contextName == "" {
		contextName = runtimeSiteKey
	}
	if projectDir == "" {
		projectDir = path.Join(managedRuntimeAppsRoot, runtimeSiteKey)
	}
	if pluginName == "" {
		pluginName = defaultPluginForSite(env.site)
		if pluginName == "" {
			return fmt.Errorf("site application type %q does not map to a supported sitectl plugin; pass --plugin explicitly", env.site.GetApplicationType())
		}
	}
	database, ok := managedDatabaseContractForSite(env.site)
	if !ok {
		return fmt.Errorf("site application type %q does not map to a supported database contract; pass a supported application type", env.site.GetApplicationType())
	}
	if sshHost == "" {
		sshHost = preferredSSHHostname(env.domains)
	}
	if sshUser == "" {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		sshUser, err = resolveManagedRuntimeSSHUser(cmd.Context(), apiBaseURL, "")
		if err != nil {
			return err
		}
	}
	if sshKey == "" {
		sshKey = defaultSSHKeyPath()
	}
	if sshPort == 0 {
		sshPort = 22
	}
	if sshHost == "" {
		return missingSSHHostnameError()
	}

	ctx := &sitectlconfig.Context{
		Name:                   contextName,
		Site:                   env.site.GetSiteId(),
		Plugin:                 pluginName,
		DockerHostType:         sitectlconfig.ContextRemote,
		Environment:            managedRuntimeEnvironment(env.site),
		DockerSocket:           "/var/run/docker.sock",
		ComposeProjectName:     runtimeSiteKey,
		ComposeNetwork:         runtimeSiteKey + "_default",
		ProjectDir:             projectDir,
		SSHUser:                sshUser,
		SSHHostname:            sshHost,
		SSHPort:                sshPort,
		SSHKeyPath:             sshKey,
		EnvFile:                []string{".env"},
		ComposeFile:            []string{valueOrDefault(env.site.GetComposeFile(), "compose.yaml")},
		DatabaseService:        database.service,
		DatabaseUser:           database.user,
		DatabaseName:           database.name,
		DatabasePasswordSecret: database.passwordSecret,
	}

	if err := sitectlconfig.SaveContext(ctx, setDefault); err != nil {
		return fmt.Errorf("save sitectl context: %w", err)
	}
	configPath, _ := sitectlconfig.ConfigFilePath()
	fmt.Printf("Updated sitectl context %q in %s\n", contextName, configPath)
	return nil
}

func managedRuntimeSiteKey(siteID string) string {
	siteID = strings.ToLower(strings.TrimSpace(siteID))
	if siteID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(siteID))
	return "site-" + hex.EncodeToString(digest[:])[:20]
}

func preferredSiteDomain(domains []*commonv1.DomainConfig) string {
	for _, domain := range domains {
		if domain == nil {
			continue
		}
		if hostname := normalizeHostname(domain.GetManagedHostname()); hostname != "" {
			return hostname
		}
	}
	for _, domain := range domains {
		if domain == nil || domain.GetKind() != commonv1.DomainKind_DOMAIN_KIND_MANAGED {
			continue
		}
		if hostname := normalizeHostname(domain.GetHostname()); hostname != "" {
			return hostname
		}
	}
	for _, domain := range domains {
		if domain == nil || !domain.GetRouteReady() {
			continue
		}
		if hostname := normalizeHostname(domain.GetHostname()); hostname != "" {
			return hostname
		}
	}
	for _, domain := range domains {
		if domain == nil {
			continue
		}
		if hostname := normalizeHostname(domain.GetHostname()); hostname != "" {
			return hostname
		}
	}
	return ""
}

func preferredSSHHostname(domains []*commonv1.DomainConfig) string {
	for _, domain := range domains {
		if domain == nil || domain.GetKind() != commonv1.DomainKind_DOMAIN_KIND_MANAGED {
			continue
		}
		if hostname := strings.TrimSpace(domain.GetSshHostname()); hostname != "" {
			return hostname
		}
	}
	for _, domain := range domains {
		if domain == nil {
			continue
		}
		if hostname := strings.TrimSpace(domain.GetSshHostname()); hostname != "" {
			return hostname
		}
	}
	return ""
}

func normalizeHostname(hostname string) string {
	return strings.TrimSuffix(strings.TrimSpace(hostname), ".")
}

func missingSSHHostnameError() error {
	return fmt.Errorf("LibOps API has not returned an SSH hostname for this site; wait for managed-domain provisioning or pass --ssh-host")
}

func looksLikeURL(value string) bool {
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return true
	}
	if strings.Contains(value, ".") && !strings.Contains(value, "/") {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && parsed.Scheme != ""
}

func normalizeGitRemote(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" || strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") {
		return repo
	}
	repo = strings.TrimPrefix(repo, "github.com/")
	if strings.Count(repo, "/") == 1 {
		return "git@github.com:" + strings.TrimSuffix(repo, ".git") + ".git"
	}
	return repo
}

func normalizeGitRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "refs/")
	ref = strings.TrimPrefix(ref, "heads/")
	ref = strings.TrimPrefix(ref, "tags/")
	if ref == "release" {
		return ""
	}
	return ref
}

func defaultCheckoutDir(repo, siteName string) string {
	repo = strings.TrimSuffix(repo, ".git")
	if idx := strings.LastIndexAny(repo, "/:"); idx >= 0 && idx+1 < len(repo) {
		return repo[idx+1:]
	}
	return sanitizeContextPart(siteName)
}

func sanitizeContextPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "site"
	}
	return out
}

func defaultPluginForSite(site *commonv1.SiteConfig) string {
	appType := strings.ToLower(strings.TrimSpace(site.GetApplicationType()))
	switch appType {
	case "archivesspace", "drupal", "ojs", "omeka-classic", "omeka-s":
		return appType
	case "wordpress", "wp":
		return "wp"
	case "islandora", "isle":
		return "isle"
	default:
		return ""
	}
}

type managedDatabaseContract struct {
	service        string
	user           string
	name           string
	passwordSecret string
}

func managedDatabaseContractForSite(site *commonv1.SiteConfig) (managedDatabaseContract, bool) {
	appType := strings.ToLower(strings.TrimSpace(site.GetApplicationType()))
	switch appType {
	case "archivesspace":
		return managedDatabaseContract{service: "mariadb", user: "archivesspace", name: "archivesspace", passwordSecret: "ARCHIVESSPACE_DB_PASSWORD"}, true
	case "drupal":
		return managedDatabaseContract{service: "mariadb", user: "drupal", name: "drupal", passwordSecret: "DRUPAL_DEFAULT_DB_PASSWORD"}, true
	case "islandora", "isle":
		return managedDatabaseContract{service: "mariadb", user: "drupal_default", name: "drupal_default", passwordSecret: "DRUPAL_DEFAULT_DB_PASSWORD"}, true
	case "ojs":
		return managedDatabaseContract{service: "mariadb", user: "ojs", name: "ojs", passwordSecret: "OJS_DB_PASSWORD"}, true
	case "omeka-classic":
		return managedDatabaseContract{service: "mariadb", user: "omeka_classic", name: "omeka_classic", passwordSecret: "OMEKA_CLASSIC_DB_PASSWORD"}, true
	case "omeka-s":
		return managedDatabaseContract{service: "mariadb", user: "omeka_s", name: "omeka_s", passwordSecret: "OMEKA_S_DB_PASSWORD"}, true
	case "wordpress", "wp":
		return managedDatabaseContract{service: "mariadb", user: "wordpress", name: "wordpress", passwordSecret: "WORDPRESS_DB_PASSWORD"}, true
	default:
		return managedDatabaseContract{}, false
	}
}

func managedRuntimeEnvironment(site *commonv1.SiteConfig) string {
	if site != nil && site.GetIsProduction() {
		return "production"
	}
	return "non-production"
}

func resolveManagedRuntimeSSHUser(ctx context.Context, apiBaseURL, requested string) (string, error) {
	if requested = strings.TrimSpace(requested); requested != "" {
		return requested, nil
	}

	account, err := api.GetCurrentAccount(ctx, apiBaseURL)
	if err != nil {
		return "", fmt.Errorf("resolve managed runtime SSH account: %w", err)
	}
	return managedRuntimeSSHAccountID(account)
}

func managedRuntimeSSHAccountID(account *api.CurrentAccount) (string, error) {
	if account == nil {
		return "", fmt.Errorf("resolve managed runtime SSH account: LibOps API returned no account")
	}

	accountID := account.ID
	parsed, err := uuid.Parse(accountID)
	if err != nil || parsed == uuid.Nil || parsed.String() != accountID || accountID != strings.TrimSpace(accountID) {
		return "", fmt.Errorf("resolve managed runtime SSH account: LibOps API returned invalid account ID %q", account.ID)
	}
	return accountID, nil
}

func defaultSSHKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		path := filepath.Join(home, ".ssh", name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(home, ".ssh", "id_ed25519")
	}
	return filepath.Join(home, ".ssh", "id_ed25519")
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func addSiteRuntimeContextFlags(cmd *cobra.Command) {
	cmd.Flags().String("context-name", "", "sitectl context name")
	cmd.Flags().String("project-dir", "", "Remote Compose checkout directory on the managed host; defaults from the site ID")
	cmd.Flags().String("plugin", "", "sitectl plugin name; defaults from site application type")
	cmd.Flags().String("ssh-host", "", "SSH hostname override; defaults to the exact hostname returned by the LibOps API")
	cmd.Flags().String("ssh-user", "", "Linux account override; defaults to the authenticated LibOps account UUID provisioned on the managed host")
	cmd.Flags().Uint("ssh-port", 22, "SSH port")
	cmd.Flags().String("ssh-key", "", "Local private key used to authenticate the generated sitectl context to the managed host.")
	cmd.Flags().Bool("default", true, "Set the updated context as current")
}

func init() {
	pingCmd.Flags().Duration("timeout", 30*time.Second, "HTTP timeout")

	sshCmd.Flags().String("ssh-host", "", "SSH hostname override; defaults to the exact hostname returned by the LibOps API")
	sshCmd.Flags().String("ssh-user", "", "Linux account override; defaults to the authenticated LibOps account UUID provisioned on the managed host")
	sshCmd.Flags().Uint("ssh-port", 22, "SSH port")
	sshCmd.Flags().String("ssh-key", "", "Local private key used to authenticate the SSH session to the managed host.")
	sshCmd.Flags().SetInterspersed(false)

	checkoutCmd.Flags().Bool("update-context", false, "Update a sitectl context after checkout")
	addSiteRuntimeContextFlags(checkoutCmd)

	contextCmd.AddCommand(contextUpdateCmd)
	addSiteRuntimeContextFlags(contextUpdateCmd)
}
