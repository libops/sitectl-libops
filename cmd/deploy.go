package cmd

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	libopsv1 "github.com/libops/proto/libops/v1"
	"github.com/libops/sitectl-libops/pkg/api"
	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy LibOps resources",
}

type deployLifecycleOptions struct {
	Environment         string
	ContextName         string
	WorkingDir          string
	SkipHealthcheck     bool
	HealthcheckTimeout  time.Duration
	HealthcheckInterval time.Duration
	SkipVerify          bool
	ForceVerify         bool
	VerifyArgs          []string
}

type commandExecutor func(ctx context.Context, stdout, stderr io.Writer, workdir, name string, args ...string) error

const deploymentStatusIDHeader = "X-LibOps-Deployment-ID"

var deploySiteCmd = &cobra.Command{
	Use:   "site <site-id>",
	Short: "Deploy a site and run post-deploy checks",
	Long: `Deploy one immutable commit on the site's configured branch through the
LibOps API, then run the same post-deploy checks used by CI.

The full commit SHA is required. If request delivery is uncertain, retry with
the same --request-id so the API returns the original deployment.

The command validates the deployment ID returned by the API, uses it for every
status request, and checks the response echo so another deployment of the same
site cannot satisfy the wait.

The command always runs sitectl healthcheck unless --skip-healthcheck is set.
For non-production sites it also runs sitectl verify. Production sites only run
the basic healthcheck unless --verify is explicitly set.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		siteID := strings.TrimSpace(args[0])
		gitRef := strings.TrimSpace(mustGetStringFlag(cmd, "git-ref"))
		commitSHA, err := normalizedDeploymentCommitSHA(mustGetStringFlag(cmd, "commit-sha"))
		if err != nil {
			return err
		}
		requestID, err := normalizedDeploymentRequestID(mustGetStringFlag(cmd, "request-id"))
		if err != nil {
			return err
		}
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		environment, err := resolveDeployEnvironment(cmd.Context(), client, siteID, mustGetStringFlag(cmd, "environment"))
		if err != nil {
			return err
		}
		skipDeploymentWait := mustGetBoolFlag(cmd, "skip-deployment-wait")

		request := &libopsv1.DeploySiteRequest{SiteId: siteID}
		if gitRef != "" {
			request.GitRef = &gitRef
		}

		connectRequest := connect.NewRequest(request)
		connectRequest.Header().Set("X-LibOps-Commit-SHA", commitSHA)
		connectRequest.Header().Set("Idempotency-Key", "cli-deploy:"+requestID)
		resp, err := client.SiteOperations.DeploySite(cmd.Context(), connectRequest)
		if err != nil {
			return fmt.Errorf("failed to trigger deployment (retry with --request-id %s): %w", requestID, err)
		}
		deploymentID, err := normalizedDeploymentReceiptID(resp.Msg.GetDeploymentId())
		if err != nil {
			return fmt.Errorf("LibOps API returned an invalid deployment ID (retry with --request-id %s): %w", requestID, err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Triggered deployment: %s\n", deploymentID)
		fmt.Fprintf(cmd.OutOrStdout(), "Request ID: %s\n", requestID)
		if status := resp.Msg.GetStatus(); status != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Status: %s", status.GetStatus())
			if message := strings.TrimSpace(status.GetMessage()); message != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " - %s", message)
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}

		if !skipDeploymentWait {
			if _, err := waitForSiteDeployment(cmd.Context(), client, siteID, deploymentID, mustGetDurationFlag(cmd, "deployment-timeout"), mustGetDurationFlag(cmd, "deployment-interval"), cmd.OutOrStdout()); err != nil {
				return err
			}
		}

		opts := deployLifecycleOptions{
			Environment:         environment,
			ContextName:         strings.TrimSpace(mustGetStringFlag(cmd, "context")),
			WorkingDir:          strings.TrimSpace(mustGetStringFlag(cmd, "working-dir")),
			SkipHealthcheck:     mustGetBoolFlag(cmd, "skip-healthcheck"),
			HealthcheckTimeout:  mustGetDurationFlag(cmd, "healthcheck-timeout"),
			HealthcheckInterval: mustGetDurationFlag(cmd, "healthcheck-interval"),
			SkipVerify:          mustGetBoolFlag(cmd, "skip-verify"),
			ForceVerify:         mustGetBoolFlag(cmd, "verify"),
			VerifyArgs:          mustGetStringArrayFlag(cmd, "verify-arg"),
		}
		return runDeployLifecycleChecks(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, execCommand)
	},
}

func init() {
	deployCmd.AddCommand(deploySiteCmd)

	deploySiteCmd.Flags().String("git-ref", "", "Configured branch ref to deploy; omit to use the site's stored branch")
	deploySiteCmd.Flags().String("commit-sha", "", "Full 40- or 64-character commit SHA to deploy")
	deploySiteCmd.Flags().String("request-id", "", "Stable UUID for safely retrying this deployment request")
	deploySiteCmd.Flags().String("environment", "auto", "Deployment environment: auto, production, staging, development, preview, or another non-production label")
	deploySiteCmd.Flags().String("working-dir", "", "Directory where sitectl healthcheck and verify should run")
	deploySiteCmd.Flags().Bool("skip-deployment-wait", false, "Skip waiting for the exact LibOps deployment receipt before post-deploy checks")
	deploySiteCmd.Flags().Duration("deployment-timeout", 20*time.Minute, "Maximum time to wait for the exact LibOps deployment receipt")
	deploySiteCmd.Flags().Duration("deployment-interval", 15*time.Second, "Delay between exact LibOps deployment receipt checks")
	deploySiteCmd.Flags().Bool("skip-healthcheck", false, "Skip post-deploy sitectl healthcheck")
	deploySiteCmd.Flags().Duration("healthcheck-timeout", 10*time.Minute, "Maximum time to wait for sitectl healthcheck")
	deploySiteCmd.Flags().Duration("healthcheck-interval", 15*time.Second, "Delay between persistent healthcheck attempts")
	deploySiteCmd.Flags().Bool("skip-verify", false, "Skip sitectl verify for non-production deployments")
	deploySiteCmd.Flags().Bool("verify", false, "Run sitectl verify even for production deployments")
	deploySiteCmd.Flags().StringArray("verify-arg", nil, "Additional argument passed to sitectl verify; repeat for multiple arguments")
	_ = deploySiteCmd.MarkFlagRequired("commit-sha")
}

func normalizedDeploymentCommitSHA(raw string) (string, error) {
	commitSHA := strings.ToLower(strings.TrimSpace(raw))
	if len(commitSHA) != 40 && len(commitSHA) != 64 {
		return "", fmt.Errorf("commit-sha must be a full 40- or 64-character hexadecimal commit SHA")
	}
	if _, err := hex.DecodeString(commitSHA); err != nil {
		return "", fmt.Errorf("commit-sha must be hexadecimal")
	}
	return commitSHA, nil
}

func normalizedDeploymentRequestID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.NewString(), nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("request-id must be a UUID")
	}
	return parsed.String(), nil
}

func normalizedDeploymentReceiptID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil {
		return "", fmt.Errorf("deployment ID must be a UUID")
	}
	return parsed.String(), nil
}

func validateDeploymentReceiptEcho(raw, expected string) error {
	actual, err := normalizedDeploymentReceiptID(raw)
	if err != nil {
		return fmt.Errorf("%s response header is invalid: %w", deploymentStatusIDHeader, err)
	}
	if actual != expected {
		return fmt.Errorf("%s response header selected deployment %s instead of %s", deploymentStatusIDHeader, actual, expected)
	}
	return nil
}

func resolveDeployEnvironment(ctx context.Context, client *api.LibopsAPIClient, siteID, requested string) (string, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested != "" && requested != "auto" {
		return requested, nil
	}

	resp, err := client.SiteService.GetSite(ctx, connect.NewRequest(&libopsv1.GetSiteRequest{SiteId: siteID}))
	if err != nil {
		return "", fmt.Errorf("failed to resolve site environment: %w", err)
	}
	site := resp.Msg.GetSite()
	if site == nil {
		return "", fmt.Errorf("failed to resolve site environment: site %q was not returned by the API", siteID)
	}
	return managedRuntimeEnvironment(site), nil
}

func waitForSiteDeployment(ctx context.Context, client *api.LibopsAPIClient, siteID, deploymentID string, timeout, interval time.Duration, stdout io.Writer) (*libopsv1.SiteStatus, error) {
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastStatusFingerprint string
	for {
		statusRequest := connect.NewRequest(&libopsv1.GetSiteStatusRequest{SiteId: siteID})
		statusRequest.Header().Set(deploymentStatusIDHeader, deploymentID)
		resp, err := client.SiteOperations.GetSiteStatus(waitCtx, statusRequest)
		if err != nil {
			return nil, fmt.Errorf("failed to read deployment status: %w", err)
		}
		if err := validateDeploymentReceiptEcho(resp.Header().Get(deploymentStatusIDHeader), deploymentID); err != nil {
			return nil, fmt.Errorf("failed to verify exact deployment receipt: %w", err)
		}
		status := resp.Msg.GetStatus()
		statusValue := ""
		if status != nil {
			statusValue = strings.ToLower(strings.TrimSpace(status.GetStatus()))
		}
		statusFingerprint := statusValue
		if status != nil {
			statusFingerprint += "\x00" + strings.TrimSpace(status.GetDeployedAt()) + "\x00" + strings.TrimSpace(status.GetMessage())
		}
		if statusFingerprint != lastStatusFingerprint {
			if statusValue == "" {
				fmt.Fprintln(stdout, "Deployment status: unknown")
			} else if message := strings.TrimSpace(status.GetMessage()); message != "" {
				fmt.Fprintf(stdout, "Deployment status: %s - %s\n", status.GetStatus(), message)
			} else {
				fmt.Fprintf(stdout, "Deployment status: %s\n", status.GetStatus())
			}
			lastStatusFingerprint = statusFingerprint
		}

		switch deploymentStatusDisposition(statusValue) {
		case deploymentStatusSucceeded:
			return status, nil
		case deploymentStatusFailed:
			message := "deployment failed"
			if status != nil && strings.TrimSpace(status.GetMessage()) != "" {
				message = strings.TrimSpace(status.GetMessage())
			}
			return status, fmt.Errorf("deployment failed: %s", message)
		case deploymentStatusInvalid:
			return status, fmt.Errorf("deployment %s returned unsupported receipt status %q", deploymentID, statusValue)
		}

		timer := time.NewTimer(interval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return status, fmt.Errorf("deployment did not complete within %s: %w", timeout, waitCtx.Err())
		case <-timer.C:
		}
	}
}

type deploymentStatusResult string

const (
	deploymentStatusPending   deploymentStatusResult = "pending"
	deploymentStatusSucceeded deploymentStatusResult = "succeeded"
	deploymentStatusFailed    deploymentStatusResult = "failed"
	deploymentStatusInvalid   deploymentStatusResult = "invalid"
)

func deploymentStatusDisposition(status string) deploymentStatusResult {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "deploying":
		return deploymentStatusPending
	case "deployed":
		return deploymentStatusSucceeded
	case "failed", "error", "errored", "superseded", "canceled", "cancelled":
		return deploymentStatusFailed
	default:
		return deploymentStatusInvalid
	}
}

func runDeployLifecycleChecks(ctx context.Context, stdout, stderr io.Writer, opts deployLifecycleOptions, execer commandExecutor) error {
	if opts.SkipVerify && opts.ForceVerify {
		return fmt.Errorf("--skip-verify and --verify cannot both be set")
	}
	if execer == nil {
		execer = execCommand
	}

	if !opts.SkipHealthcheck {
		healthcheckArgs := buildHealthcheckArgs(opts)
		fmt.Fprintf(stdout, "Running sitectl %s\n", strings.Join(healthcheckArgs, " "))
		if err := execer(ctx, stdout, stderr, opts.WorkingDir, "sitectl", healthcheckArgs...); err != nil {
			return fmt.Errorf("post-deploy healthcheck failed: %w", err)
		}
	}

	if shouldRunDeployVerify(opts) {
		verifyArgs := buildVerifyArgs(opts)
		fmt.Fprintf(stdout, "Running sitectl %s\n", strings.Join(verifyArgs, " "))
		if err := execer(ctx, stdout, stderr, opts.WorkingDir, "sitectl", verifyArgs...); err != nil {
			return fmt.Errorf("post-deploy verify failed: %w", err)
		}
	}

	return nil
}

func shouldRunDeployVerify(opts deployLifecycleOptions) bool {
	if opts.SkipVerify {
		return false
	}
	if opts.ForceVerify {
		return true
	}
	return !isProductionEnvironment(opts.Environment)
}

func isProductionEnvironment(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "prod", "production", "live":
		return true
	default:
		return false
	}
}

func buildHealthcheckArgs(opts deployLifecycleOptions) []string {
	args := []string{"healthcheck", "--persist"}
	if strings.TrimSpace(opts.ContextName) != "" {
		args = append(args, "--context", strings.TrimSpace(opts.ContextName))
	}
	if opts.HealthcheckTimeout > 0 {
		args = append(args, "--timeout", opts.HealthcheckTimeout.String())
	}
	if opts.HealthcheckInterval > 0 {
		args = append(args, "--interval", opts.HealthcheckInterval.String())
	}
	return args
}

func buildVerifyArgs(opts deployLifecycleOptions) []string {
	args := []string{"verify"}
	if strings.TrimSpace(opts.ContextName) != "" {
		args = append(args, "--context", strings.TrimSpace(opts.ContextName))
	}
	args = append(args, opts.VerifyArgs...)
	return args
}

func execCommand(ctx context.Context, stdout, stderr io.Writer, workdir, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...) // #nosec G204 -- arguments are explicit sitectl subcommands assembled by sitectl-libops.
	if strings.TrimSpace(workdir) != "" {
		command.Dir = workdir
	}
	command.Env = os.Environ()
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func mustGetStringFlag(cmd *cobra.Command, name string) string {
	value, err := cmd.Flags().GetString(name)
	if err != nil {
		panic(err)
	}
	return value
}

func mustGetBoolFlag(cmd *cobra.Command, name string) bool {
	value, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(err)
	}
	return value
}

func mustGetDurationFlag(cmd *cobra.Command, name string) time.Duration {
	value, err := cmd.Flags().GetDuration(name)
	if err != nil {
		panic(err)
	}
	return value
}

func mustGetStringArrayFlag(cmd *cobra.Command, name string) []string {
	value, err := cmd.Flags().GetStringArray(name)
	if err != nil {
		panic(err)
	}
	return value
}
