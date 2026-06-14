package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

type deployExecCall struct {
	workdir string
	name    string
	args    []string
}

func TestRunDeployLifecycleChecksRunsVerifyForNonProduction(t *testing.T) {
	var calls []deployExecCall
	execer := func(ctx context.Context, stdout, stderr io.Writer, workdir, name string, args ...string) error {
		calls = append(calls, deployExecCall{workdir: workdir, name: name, args: append([]string{}, args...)})
		return nil
	}

	opts := deployLifecycleOptions{
		Environment:         "staging",
		ContextName:         "stage",
		WorkingDir:          "/srv/site",
		HealthcheckTimeout:  2 * time.Minute,
		HealthcheckInterval: 5 * time.Second,
		VerifyArgs:          []string{"--bot-mitigation", "on"},
	}
	if err := runDeployLifecycleChecks(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, opts, execer); err != nil {
		t.Fatalf("runDeployLifecycleChecks() error = %v", err)
	}

	want := []deployExecCall{
		{
			workdir: "/srv/site",
			name:    "sitectl",
			args:    []string{"healthcheck", "--persist", "--context", "stage", "--timeout", "2m0s", "--interval", "5s"},
		},
		{
			workdir: "/srv/site",
			name:    "sitectl",
			args:    []string{"verify", "--context", "stage", "--bot-mitigation", "on"},
		},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRunDeployLifecycleChecksSkipsVerifyForProduction(t *testing.T) {
	var calls []deployExecCall
	opts := deployLifecycleOptions{
		Environment:         "production",
		HealthcheckTimeout:  time.Minute,
		HealthcheckInterval: time.Second,
	}
	err := runDeployLifecycleChecks(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, opts, func(ctx context.Context, stdout, stderr io.Writer, workdir, name string, args ...string) error {
		calls = append(calls, deployExecCall{name: name, args: append([]string{}, args...)})
		return nil
	})
	if err != nil {
		t.Fatalf("runDeployLifecycleChecks() error = %v", err)
	}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].args, []string{"healthcheck", "--persist", "--timeout", "1m0s", "--interval", "1s"}) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRunDeployLifecycleChecksForceVerifyForProduction(t *testing.T) {
	var calls []deployExecCall
	opts := deployLifecycleOptions{
		Environment:         "prod",
		ForceVerify:         true,
		HealthcheckTimeout:  time.Minute,
		HealthcheckInterval: time.Second,
	}
	err := runDeployLifecycleChecks(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, opts, func(ctx context.Context, stdout, stderr io.Writer, workdir, name string, args ...string) error {
		calls = append(calls, deployExecCall{name: name, args: append([]string{}, args...)})
		return nil
	})
	if err != nil {
		t.Fatalf("runDeployLifecycleChecks() error = %v", err)
	}
	if len(calls) != 2 || calls[1].args[0] != "verify" {
		t.Fatalf("calls = %#v, want healthcheck followed by verify", calls)
	}
}

func TestRunDeployLifecycleChecksRejectsVerifyConflict(t *testing.T) {
	err := runDeployLifecycleChecks(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, deployLifecycleOptions{
		Environment: "staging",
		SkipVerify:  true,
		ForceVerify: true,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "--skip-verify and --verify") {
		t.Fatalf("error = %v, want verify conflict", err)
	}
}

func TestRunDeployLifecycleChecksReturnsHealthcheckFailure(t *testing.T) {
	wantErr := errors.New("healthcheck failed")
	err := runDeployLifecycleChecks(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, deployLifecycleOptions{
		Environment:         "staging",
		HealthcheckTimeout:  time.Minute,
		HealthcheckInterval: time.Second,
	}, func(ctx context.Context, stdout, stderr io.Writer, workdir, name string, args ...string) error {
		return wantErr
	})
	if err == nil || !strings.Contains(err.Error(), "post-deploy healthcheck failed") {
		t.Fatalf("error = %v, want healthcheck failure", err)
	}
}

func TestIsProductionEnvironment(t *testing.T) {
	for _, value := range []string{"production", "prod", "live", " Production "} {
		if !isProductionEnvironment(value) {
			t.Fatalf("isProductionEnvironment(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "staging", "development", "preview"} {
		if isProductionEnvironment(value) {
			t.Fatalf("isProductionEnvironment(%q) = true, want false", value)
		}
	}
}

func TestDeploymentStatusDisposition(t *testing.T) {
	tests := []struct {
		status string
		want   deploymentStatusResult
	}{
		{status: "deploying", want: deploymentStatusPending},
		{status: "pending", want: deploymentStatusPending},
		{status: "deployed", want: deploymentStatusSucceeded},
		{status: "completed", want: deploymentStatusSucceeded},
		{status: "failed", want: deploymentStatusFailed},
		{status: "error", want: deploymentStatusFailed},
	}
	for _, tt := range tests {
		if got := deploymentStatusDisposition(tt.status); got != tt.want {
			t.Fatalf("deploymentStatusDisposition(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}
