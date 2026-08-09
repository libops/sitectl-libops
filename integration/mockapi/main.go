package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	libopsv1 "github.com/libops/proto/libops/v1"
	commonv1 "github.com/libops/proto/libops/v1/common"
	"github.com/libops/proto/libops/v1/libopsv1connect"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	expectedAuthorization = "Bearer integration-test-api-key"
	expectedOrganization  = "11111111-1111-4111-8111-111111111111"
	expectedProject       = "22222222-2222-4222-8222-222222222222"
	expectedSite          = "33333333-3333-4333-8333-333333333333"
	expectedRequestID     = "5c5e38ea-1b95-4fa3-b248-94caa88f954b"
	expectedMessage       = "add an institution-specific publication search"
	expectedDeploymentID  = "55555555-5555-4555-8555-555555555555"
	deploymentIDHeader    = "X-LibOps-Deployment-ID"
	taskID                = "44444444-4444-4444-8444-444444444444"
)

type contractState struct {
	receiptFile string
	mu          sync.Mutex
	siteCreated bool
	deployed    bool
	statusRead  bool
	taskRead    bool
}

type assistantService struct {
	libopsv1connect.UnimplementedAssistantServiceHandler
}

func (s *assistantService) Chat(_ context.Context, req *connect.Request[libopsv1.AssistantChatRequest]) (*connect.Response[libopsv1.AssistantChatResponse], error) {
	if err := validateAuthorization(req.Header()); err != nil {
		return nil, err
	}
	message := req.Msg
	if message.GetOrganizationId() != expectedOrganization || message.GetProjectId() != expectedProject || message.GetSiteId() != expectedSite {
		return nil, invalidContract("unexpected Task Agent scope")
	}
	if message.GetMessage() != expectedMessage {
		return nil, invalidContract("unexpected Task Agent message")
	}
	if message.GetClientRequestId() != expectedRequestID {
		return nil, invalidContract("unexpected Task Agent request ID")
	}
	if message.GetAgentModel() != "glm-5.2:cloud" {
		return nil, invalidContract("unexpected Task Agent model")
	}
	if message.GetHarness() != libopsv1.TaskHarness_TASK_HARNESS_CODEX {
		return nil, invalidContract("unexpected Task Agent harness")
	}
	if message.GetMetadata()["conversation_provider"] != "cli" || message.GetMetadata()["conversation_response_target"] != "cli_poll" {
		return nil, invalidContract("unexpected Task Agent delivery metadata")
	}

	return connect.NewResponse(&libopsv1.AssistantChatResponse{
		RequestId: taskID,
		Status:    "queued",
		Reply:     "The first-customer site change is queued.",
	}), nil
}

type siteService struct {
	libopsv1connect.UnimplementedSiteServiceHandler
	state *contractState
}

func (s *siteService) CreateSite(_ context.Context, req *connect.Request[libopsv1.CreateSiteRequest]) (*connect.Response[libopsv1.CreateSiteResponse], error) {
	if err := validateAuthorization(req.Header()); err != nil {
		return nil, err
	}
	site := req.Msg.GetSite()
	if req.Msg.GetProjectId() != expectedProject || site == nil {
		return nil, invalidContract("unexpected site create scope")
	}
	if site.GetSiteName() != "production" || site.GetGithubRepository() != "https://github.com/libops/isle" || site.GetGithubRef() != "" {
		return nil, invalidContract("unexpected site source contract")
	}
	if site.GetComposePath() != "." || site.GetComposeFile() != "compose.yaml" {
		return nil, invalidContract("unexpected site Compose contract")
	}
	if site.GetPort() != 80 || site.GetApplicationType() != "islandora" {
		return nil, invalidContract("unexpected site application contract")
	}
	if len(site.GetInitCmd()) != 0 || len(site.GetUpCmd()) != 0 || len(site.GetRolloutCmd()) != 0 {
		return nil, invalidContract("site lifecycle commands must use the canonical template defaults")
	}
	if err := s.state.markSiteCreated(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&libopsv1.CreateSiteResponse{Site: &commonv1.SiteConfig{
		SiteId:           expectedSite,
		OrganizationId:   expectedOrganization,
		ProjectId:        expectedProject,
		SiteName:         site.GetSiteName(),
		GithubRepository: "https://github.com/libops/example-site",
		GithubRef:        "heads/main",
		ComposePath:      site.GetComposePath(),
		ComposeFile:      site.GetComposeFile(),
		Port:             site.GetPort(),
		ApplicationType:  site.GetApplicationType(),
		IsProduction:     true,
	}}), nil
}

type siteOperationsService struct {
	libopsv1connect.UnimplementedSiteOperationsServiceHandler
	state *contractState
}

func (s *siteOperationsService) DeploySite(_ context.Context, req *connect.Request[libopsv1.DeploySiteRequest]) (*connect.Response[libopsv1.DeploySiteResponse], error) {
	if err := validateAuthorization(req.Header()); err != nil {
		return nil, err
	}
	if req.Msg.GetSiteId() != expectedSite || req.Msg.GetGitRef() != "heads/main" {
		return nil, invalidContract("unexpected deployment contract")
	}
	if req.Header().Get("X-LibOps-Commit-SHA") != "0123456789abcdef0123456789abcdef01234567" {
		return nil, invalidContract("unexpected deployment commit")
	}
	if req.Header().Get("Idempotency-Key") != "cli-deploy:6d1adfcb-7b77-4a93-a476-a492037725e1" {
		return nil, invalidContract("unexpected deployment idempotency key")
	}
	if err := s.state.markDeployed(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	message := "First-customer deployment is starting"
	return connect.NewResponse(&libopsv1.DeploySiteResponse{
		DeploymentId: expectedDeploymentID,
		Status: &libopsv1.SiteStatus{
			SiteId:  expectedSite,
			Status:  "deploying",
			Message: &message,
		},
	}), nil
}

func (s *siteOperationsService) GetSiteStatus(_ context.Context, req *connect.Request[libopsv1.GetSiteStatusRequest]) (*connect.Response[libopsv1.GetSiteStatusResponse], error) {
	if err := validateAuthorization(req.Header()); err != nil {
		return nil, err
	}
	if req.Msg.GetSiteId() != expectedSite {
		return nil, invalidContract("unexpected deployment status scope")
	}
	if !s.state.deploymentHasStarted() {
		return connect.NewResponse(&libopsv1.GetSiteStatusResponse{Status: &libopsv1.SiteStatus{
			SiteId: expectedSite,
			Status: "provisioning",
		}}), nil
	}
	if req.Header().Get(deploymentIDHeader) != expectedDeploymentID {
		return nil, invalidContract("deployment status did not fence the exact deployment receipt")
	}
	if err := s.state.markStatusRead(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	deployedAt := "2026-08-09T10:01:00Z"
	response := connect.NewResponse(&libopsv1.GetSiteStatusResponse{Status: &libopsv1.SiteStatus{
		SiteId:     expectedSite,
		Status:     "deployed",
		DeployedAt: &deployedAt,
	}})
	response.Header().Set(deploymentIDHeader, expectedDeploymentID)
	return response, nil
}

type taskService struct {
	libopsv1connect.UnimplementedTaskServiceHandler
	state *contractState
}

func (s *taskService) GetTask(_ context.Context, req *connect.Request[libopsv1.GetTaskRequest]) (*connect.Response[libopsv1.GetTaskResponse], error) {
	if err := validateAuthorization(req.Header()); err != nil {
		return nil, err
	}
	if req.Msg.GetOrganizationId() != expectedOrganization || req.Msg.GetTaskId() != taskID {
		return nil, invalidContract("unexpected Task Agent status lookup")
	}

	inputFields, err := structpb.NewStruct(map[string]any{
		"preview_url":              "https://preview.example.test/44444444-4444-4444-8444-444444444444",
		"task_followup_generation": 0,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resultMetadata, err := structpb.NewStruct(map[string]any{
		"summary":                  "Prepared the institution-specific site change for operator review.",
		"task_followup_generation": 0,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if err := s.state.markTaskRead(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("write contract receipt: %w", err))
	}
	return connect.NewResponse(&libopsv1.GetTaskResponse{Task: &libopsv1.Task{
		TaskId:         taskID,
		OrganizationId: expectedOrganization,
		ProjectId:      expectedProject,
		SiteId:         expectedSite,
		Status:         libopsv1.TaskStatus_TASK_STATUS_RUNNING,
		InputResponse:  &libopsv1.TaskInput{Fields: inputFields},
		Results: []*libopsv1.TaskResult{{
			Type:     libopsv1.TaskResultType_TASK_RESULT_PR_CREATED,
			PrUrl:    "https://github.com/libops/example-site/pull/123",
			Metadata: resultMetadata,
		}},
	}}), nil
}

func (s *contractState) markSiteCreated() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.siteCreated = true
	return s.writeReceiptIfComplete()
}

func (s *contractState) markDeployed() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deployed = true
	return s.writeReceiptIfComplete()
}

func (s *contractState) deploymentHasStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deployed
}

func (s *contractState) markStatusRead() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusRead = true
	return s.writeReceiptIfComplete()
}

func (s *contractState) markTaskRead() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskRead = true
	return s.writeReceiptIfComplete()
}

func (s *contractState) writeReceiptIfComplete() error {
	if !s.siteCreated || !s.deployed || !s.statusRead || !s.taskRead {
		return nil
	}
	return writeAtomic(s.receiptFile, []byte("first-customer-contract-ok\n"))
}

func validateAuthorization(header http.Header) error {
	if header.Get("Authorization") != expectedAuthorization {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("missing integration API credential"))
	}
	return nil
}

func invalidContract(message string) error {
	return connect.NewError(connect.CodeInvalidArgument, errors.New(message))
}

func writeAtomic(destination string, data []byte) error {
	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(directory, ".first-customer-contract-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryName)
	}()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, destination)
}

func main() {
	readyFile := flag.String("ready-file", "", "file that receives the loopback API URL")
	receiptFile := flag.String("receipt-file", "", "file written after the contract succeeds")
	flag.Parse()
	if strings.TrimSpace(*readyFile) == "" || strings.TrimSpace(*receiptFile) == "" {
		log.Fatal("--ready-file and --receipt-file are required")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	state := &contractState{receiptFile: *receiptFile}
	mux := http.NewServeMux()
	assistantPath, assistantHandler := libopsv1connect.NewAssistantServiceHandler(&assistantService{})
	taskPath, taskHandler := libopsv1connect.NewTaskServiceHandler(&taskService{state: state})
	sitePath, siteHandler := libopsv1connect.NewSiteServiceHandler(&siteService{state: state})
	siteOperationsPath, siteOperationsHandler := libopsv1connect.NewSiteOperationsServiceHandler(&siteOperationsService{state: state})
	mux.Handle(assistantPath, assistantHandler)
	mux.Handle(taskPath, taskHandler)
	mux.Handle(sitePath, siteHandler)
	mux.Handle(siteOperationsPath, siteOperationsHandler)

	apiURL := "http://" + listener.Addr().String() + "\n"
	if err := writeAtomic(*readyFile, []byte(apiURL)); err != nil {
		log.Fatalf("publish listener URL: %v", err)
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}
