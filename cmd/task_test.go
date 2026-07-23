package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	libopsv1 "github.com/libops/proto/libops/v1"
	"github.com/libops/proto/libops/v1/libopsv1connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/structpb"
)

type testAssistantService struct {
	libopsv1connect.UnimplementedAssistantServiceHandler
	chat func(context.Context, *connect.Request[libopsv1.AssistantChatRequest]) (*connect.Response[libopsv1.AssistantChatResponse], error)
}

func (s *testAssistantService) Chat(ctx context.Context, req *connect.Request[libopsv1.AssistantChatRequest]) (*connect.Response[libopsv1.AssistantChatResponse], error) {
	return s.chat(ctx, req)
}

type testTaskService struct {
	libopsv1connect.UnimplementedTaskServiceHandler
	getTask    func(context.Context, *connect.Request[libopsv1.GetTaskRequest]) (*connect.Response[libopsv1.GetTaskResponse], error)
	updateTask func(context.Context, *connect.Request[libopsv1.UpdateTaskRequest]) (*connect.Response[libopsv1.UpdateTaskResponse], error)
}

func (s *testTaskService) GetTask(ctx context.Context, req *connect.Request[libopsv1.GetTaskRequest]) (*connect.Response[libopsv1.GetTaskResponse], error) {
	return s.getTask(ctx, req)
}

func (s *testTaskService) UpdateTask(ctx context.Context, req *connect.Request[libopsv1.UpdateTaskRequest]) (*connect.Response[libopsv1.UpdateTaskResponse], error) {
	if s.updateTask == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
	}
	return s.updateTask(ctx, req)
}

type trackingReader struct {
	read bool
}

func (r *trackingReader) Read([]byte) (int, error) {
	r.read = true
	return 0, errors.New("unexpected input read")
}

func TestRunTaskCreateAPIActionPrintsSafeInstructionsAndExits(t *testing.T) {
	var received *libopsv1.AssistantChatRequest
	headers := mustStruct(t, map[string]any{
		"Authorization": "Bearer do-not-print",
		"Content-Type":  "application/json",
		"X-API-Key":     "also-do-not-print",
		"X-Request-ID":  "request-123",
	})
	body := mustStruct(t, map[string]any{
		"display_name":    "Documentation",
		"organization_id": "org-1",
		"settings": map[string]any{
			"enabled": true,
		},
	})

	clients := newTestTaskAPIClients(t, &testAssistantService{
		chat: func(_ context.Context, req *connect.Request[libopsv1.AssistantChatRequest]) (*connect.Response[libopsv1.AssistantChatResponse], error) {
			received = req.Msg
			return connect.NewResponse(&libopsv1.AssistantChatResponse{
				RequestId: "task-api-action",
				Status:    "queued",
				Reply:     "I prepared an API request for review.",
			}), nil
		},
	}, &testTaskService{
		getTask: func(_ context.Context, req *connect.Request[libopsv1.GetTaskRequest]) (*connect.Response[libopsv1.GetTaskResponse], error) {
			if req.Msg.GetOrganizationId() != "org-1" || req.Msg.GetTaskId() != "task-api-action" {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unexpected task lookup: %s/%s", req.Msg.GetOrganizationId(), req.Msg.GetTaskId()))
			}
			return connect.NewResponse(&libopsv1.GetTaskResponse{Task: &libopsv1.Task{
				TaskId: "task-api-action",
				Status: libopsv1.TaskStatus_TASK_STATUS_NEEDS_INPUT,
				Results: []*libopsv1.TaskResult{{
					Type: libopsv1.TaskResultType_TASK_RESULT_API_ACTION,
					ApiAction: &libopsv1.TaskApiAction{
						Method:      "post",
						Path:        "/libops.v1.ProjectService/CreateProject",
						Headers:     headers,
						Body:        body,
						Description: "Create the Documentation project.",
					},
				}},
			}}), nil
		},
	})

	cmd := newTaskCreateTestCommand(t)
	var output bytes.Buffer
	input := &trackingReader{}
	cmd.SetOut(&output)
	cmd.SetIn(input)

	if err := runTaskCreate(cmd, []string{"create", "the", "documentation", "project"}, clients); err != nil {
		t.Fatalf("runTaskCreate() error = %v", err)
	}
	if input.read {
		t.Fatal("API action attempted to read an interactive reply")
	}
	if received == nil {
		t.Fatal("assistant request was not received")
	}
	if received.GetAgentModel() != "glm-5.2:cloud" {
		t.Errorf("agent model = %q, want glm-5.2:cloud", received.GetAgentModel())
	}
	if received.GetHarness() != libopsv1.TaskHarness_TASK_HARNESS_CODEX {
		t.Errorf("harness = %s, want CODEX", received.GetHarness())
	}
	if received.GetMessage() != "create the documentation project" {
		t.Errorf("message = %q", received.GetMessage())
	}
	if _, err := uuid.Parse(received.GetClientRequestId()); err != nil {
		t.Errorf("client request ID = %q, want UUID", received.GetClientRequestId())
	}

	got := output.String()
	for _, want := range []string{
		"Created task: task-api-action",
		"Method: POST",
		"Path: /libops.v1.ProjectService/CreateProject",
		`"Content-Type": "application/json"`,
		`"X-Request-ID": "request-123"`,
		"2 secret-bearing header(s) omitted.",
		"Body:\n{\n  \"display_name\": \"Documentation\",\n  \"organization_id\": \"org-1\",\n  \"settings\": {\n    \"enabled\": true\n  }\n}",
		"credential values are never displayed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"reply>", "do-not-print", "also-do-not-print", `"Authorization"`, `"X-API-Key"`} {
		if strings.Contains(got, forbidden) {
			t.Errorf("output contains %q:\n%s", forbidden, got)
		}
	}
}

func TestTaskOutputRemovesTerminalControlAndFormattingCharacters(t *testing.T) {
	const unsafe = "safe\x1b]0;owned\a\u202espelling"
	action := &libopsv1.TaskApiAction{
		Method:      "post\x1b",
		Path:        "/libops.v1.ProjectService/CreateProject\x1b]8;;https://attacker.invalid\a",
		Description: unsafe,
		Body:        mustStruct(t, map[string]any{"display_name": unsafe}),
	}
	task := &libopsv1.Task{
		TaskId: "task-safe",
		Status: libopsv1.TaskStatus_TASK_STATUS_COMPLETED,
		Results: []*libopsv1.TaskResult{{
			Type:      libopsv1.TaskResultType_TASK_RESULT_API_ACTION,
			ApiAction: action,
			PrUrl:     "https://github.com/libops/site/pull/1\x1b]8;;https://attacker.invalid\a",
			Metadata: mustStruct(t, map[string]any{
				"summary":     unsafe,
				"preview_url": "https://preview.example/\x1b]0;owned\a",
			}),
		}},
	}

	var output bytes.Buffer
	printTaskCompletion(&output, task)
	got := output.String()
	for _, forbidden := range []string{"\x1b", "\a", "\u202e"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("task output retained terminal control %q:\n%s", forbidden, got)
		}
	}
	for _, want := range []string{"Description: safe]0;ownedspelling", "Path: /libops.v1.ProjectService/CreateProject]8;;https://attacker.invalid", "https://preview.example/]0;owned"} {
		if !strings.Contains(got, want) {
			t.Errorf("sanitized output missing %q:\n%s", want, got)
		}
	}
}

func TestTaskAPIActionRequiresAPIActionResultType(t *testing.T) {
	task := &libopsv1.Task{Results: []*libopsv1.TaskResult{{
		Type:      libopsv1.TaskResultType_TASK_RESULT_PR_CREATED,
		ApiAction: &libopsv1.TaskApiAction{Method: http.MethodPost, Path: "/libops.v1.ProjectService/CreateProject"},
	}}}
	if action := taskAPIAction(task); action != nil {
		t.Fatalf("taskAPIAction() = %#v for non-API-action result", action)
	}
}

func TestRunTaskCreateRejectsUnsupportedReleaseProfile(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		harness   string
		wantError string
	}{
		{name: "model", model: "kimi-k2.6", harness: "codex", wantError: "currently supports glm-5.2:cloud"},
		{name: "harness", model: "glm-5.2:cloud", harness: "claude", wantError: "currently supports codex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskCreateTestCommand(t)
			if err := cmd.Flags().Set("agent-model", tt.model); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Flags().Set("harness", tt.harness); err != nil {
				t.Fatal(err)
			}
			err := runTaskCreate(cmd, []string{"change", "the", "site"}, &taskAPIClients{})
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("runTaskCreate() error = %v, want containing %q", err, tt.wantError)
			}
		})
	}
}

func TestRunTaskChatSessionTerminalOutcomesReturnErrors(t *testing.T) {
	tests := []struct {
		name       string
		task       *libopsv1.Task
		wantError  string
		wantOutput string
	}{
		{
			name: "failed includes latest error log",
			task: &libopsv1.Task{
				TaskId: "task-failed",
				Status: libopsv1.TaskStatus_TASK_STATUS_FAILED,
				Logs: []*libopsv1.TaskLogEntry{
					{Level: "info", Message: "coding agent started"},
					{Level: "error", Message: "coding agent exited with status 42"},
				},
			},
			wantError:  "task task-failed failed: coding agent exited with status 42",
			wantOutput: "[TASK_STATUS_FAILED] task-failed",
		},
		{
			name: "canceled",
			task: &libopsv1.Task{
				TaskId: "task-canceled",
				Status: libopsv1.TaskStatus_TASK_STATUS_CANCELED,
			},
			wantError:  "task task-canceled was canceled",
			wantOutput: "[TASK_STATUS_CANCELED] task-canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clients := newTestTaskAPIClients(t, unimplementedTestAssistantService(), &testTaskService{
				getTask: func(_ context.Context, _ *connect.Request[libopsv1.GetTaskRequest]) (*connect.Response[libopsv1.GetTaskResponse], error) {
					return connect.NewResponse(&libopsv1.GetTaskResponse{Task: tt.task}), nil
				},
			})
			var output bytes.Buffer
			err := runTaskChatSession(context.Background(), clients, "org-1", tt.task.GetTaskId(), time.Millisecond, strings.NewReader(""), &output)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("runTaskChatSession() error = %v, want containing %q", err, tt.wantError)
			}
			if !strings.Contains(output.String(), tt.wantOutput) {
				t.Errorf("output missing %q:\n%s", tt.wantOutput, output.String())
			}
		})
	}
}

func TestRunTaskChatSessionRetriesTransientGetFailure(t *testing.T) {
	var calls atomic.Int32
	clients := newTestTaskAPIClients(t, unimplementedTestAssistantService(), &testTaskService{
		getTask: func(_ context.Context, _ *connect.Request[libopsv1.GetTaskRequest]) (*connect.Response[libopsv1.GetTaskResponse], error) {
			if calls.Add(1) == 1 {
				return nil, connect.NewError(connect.CodeUnavailable, errors.New("temporary outage"))
			}
			return connect.NewResponse(&libopsv1.GetTaskResponse{Task: &libopsv1.Task{
				TaskId: "task-complete",
				Status: libopsv1.TaskStatus_TASK_STATUS_COMPLETED,
			}}), nil
		},
	})

	var output bytes.Buffer
	err := runTaskChatSession(context.Background(), clients, "org-1", "task-complete", time.Millisecond, strings.NewReader(""), &output)
	if err != nil {
		t.Fatalf("runTaskChatSession() error = %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("GetTask calls = %d, want 2", calls.Load())
	}
	if !strings.Contains(output.String(), "LibOps task `task-com` is ready.") {
		t.Errorf("completion output missing:\n%s", output.String())
	}
}

func TestRunTaskChatSessionReturnsWhenCurrentPullRequestIsReady(t *testing.T) {
	task := &libopsv1.Task{
		TaskId:        "task-pr-ready",
		Status:        libopsv1.TaskStatus_TASK_STATUS_RUNNING,
		InputResponse: &libopsv1.TaskInput{Fields: mustStruct(t, map[string]any{"task_followup_generation": 2})},
		Results: []*libopsv1.TaskResult{{
			Type:     libopsv1.TaskResultType_TASK_RESULT_PR_CREATED,
			PrUrl:    "https://github.com/libops/site/pull/12",
			Metadata: mustStruct(t, map[string]any{"task_followup_generation": 2}),
		}},
	}
	clients := newTestTaskAPIClients(t, unimplementedTestAssistantService(), &testTaskService{
		getTask: func(context.Context, *connect.Request[libopsv1.GetTaskRequest]) (*connect.Response[libopsv1.GetTaskResponse], error) {
			return connect.NewResponse(&libopsv1.GetTaskResponse{Task: task}), nil
		},
	})

	var output bytes.Buffer
	if err := runTaskChatSession(context.Background(), clients, "org-1", task.GetTaskId(), time.Millisecond, strings.NewReader(""), &output); err != nil {
		t.Fatalf("runTaskChatSession() error = %v", err)
	}
	if !strings.Contains(output.String(), task.GetResults()[0].GetPrUrl()) {
		t.Fatalf("pull request was not printed:\n%s", output.String())
	}
}

func TestRunTaskChatSessionWaitsForCurrentFollowupGeneration(t *testing.T) {
	var calls atomic.Int32
	clients := newTestTaskAPIClients(t, unimplementedTestAssistantService(), &testTaskService{
		getTask: func(context.Context, *connect.Request[libopsv1.GetTaskRequest]) (*connect.Response[libopsv1.GetTaskResponse], error) {
			resultGeneration := 1
			if calls.Add(1) > 1 {
				resultGeneration = 2
			}
			return connect.NewResponse(&libopsv1.GetTaskResponse{Task: &libopsv1.Task{
				TaskId:        "task-pr-followup",
				Status:        libopsv1.TaskStatus_TASK_STATUS_RUNNING,
				InputResponse: &libopsv1.TaskInput{Fields: mustStruct(t, map[string]any{"task_followup_generation": 2})},
				Results: []*libopsv1.TaskResult{{
					Type:     libopsv1.TaskResultType_TASK_RESULT_PR_CREATED,
					PrUrl:    "https://github.com/libops/site/pull/12",
					Metadata: mustStruct(t, map[string]any{"task_followup_generation": resultGeneration}),
				}},
			}}), nil
		},
	})

	if err := runTaskChatSession(context.Background(), clients, "org-1", "task-pr-followup", time.Millisecond, strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("runTaskChatSession() error = %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("GetTask calls = %d, want 2", calls.Load())
	}
}

func TestRunTaskChatSessionInteractiveReplyUsesIdempotencyKey(t *testing.T) {
	var getCalls atomic.Int32
	var updateCalls atomic.Int32
	var gotIdempotencyKey string
	clients := newTestTaskAPIClients(t, unimplementedTestAssistantService(), &testTaskService{
		getTask: func(_ context.Context, req *connect.Request[libopsv1.GetTaskRequest]) (*connect.Response[libopsv1.GetTaskResponse], error) {
			if req.Msg.GetOrganizationId() != "org-1" || req.Msg.GetTaskId() != "task-interactive" {
				t.Fatalf("unexpected task lookup: %#v", req.Msg)
			}
			if getCalls.Add(1) == 1 {
				return connect.NewResponse(&libopsv1.GetTaskResponse{Task: &libopsv1.Task{
					TaskId:       "task-interactive",
					Status:       libopsv1.TaskStatus_TASK_STATUS_NEEDS_INPUT,
					InputRequest: &libopsv1.TaskInput{Message: "Which approach should I use?"},
				}}), nil
			}
			return connect.NewResponse(&libopsv1.GetTaskResponse{Task: &libopsv1.Task{
				TaskId: "task-interactive",
				Status: libopsv1.TaskStatus_TASK_STATUS_COMPLETED,
			}}), nil
		},
		updateTask: func(_ context.Context, req *connect.Request[libopsv1.UpdateTaskRequest]) (*connect.Response[libopsv1.UpdateTaskResponse], error) {
			updateCalls.Add(1)
			gotIdempotencyKey = req.Header().Get("Idempotency-Key")
			if req.Msg.GetOrganizationId() != "org-1" || req.Msg.GetTaskId() != "task-interactive" {
				t.Fatalf("unexpected task reply scope: %#v", req.Msg)
			}
			if req.Msg.GetStatus() != libopsv1.TaskStatus_TASK_STATUS_QUEUED || req.Msg.GetInputResponse().GetMessage() != "use the existing component" {
				t.Fatalf("unexpected task reply: %#v", req.Msg)
			}
			return connect.NewResponse(&libopsv1.UpdateTaskResponse{Task: &libopsv1.Task{TaskId: "task-interactive"}}), nil
		},
	})

	var output bytes.Buffer
	// Deliberately omit a trailing newline: piped and redirected input must still
	// send the final response returned alongside io.EOF.
	err := runTaskChatSession(context.Background(), clients, "org-1", "task-interactive", time.Millisecond, strings.NewReader("use the existing component"), &output)
	if err != nil {
		t.Fatalf("runTaskChatSession() error = %v", err)
	}
	if getCalls.Load() != 2 || updateCalls.Load() != 1 {
		t.Fatalf("GetTask calls = %d, UpdateTask calls = %d; want 2 and 1", getCalls.Load(), updateCalls.Load())
	}
	const prefix = "cli-task-reply:"
	if !strings.HasPrefix(gotIdempotencyKey, prefix) {
		t.Fatalf("Idempotency-Key = %q, want %q prefix", gotIdempotencyKey, prefix)
	}
	if _, err := uuid.Parse(strings.TrimPrefix(gotIdempotencyKey, prefix)); err != nil {
		t.Fatalf("Idempotency-Key = %q, want UUID suffix: %v", gotIdempotencyKey, err)
	}
	for _, want := range []string{"Which approach should I use?", "reply> ", "Reply sent.", "LibOps task `task-int` is ready."} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestSendTaskReplyUsesStableIdempotencyKey(t *testing.T) {
	requestID := "d4300b29-cd64-469c-b717-eba17cf9315d"
	var gotHeader string
	clients := newTestTaskAPIClients(t, unimplementedTestAssistantService(), &testTaskService{
		getTask: func(context.Context, *connect.Request[libopsv1.GetTaskRequest]) (*connect.Response[libopsv1.GetTaskResponse], error) {
			return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
		},
		updateTask: func(_ context.Context, req *connect.Request[libopsv1.UpdateTaskRequest]) (*connect.Response[libopsv1.UpdateTaskResponse], error) {
			gotHeader = req.Header().Get("Idempotency-Key")
			if req.Msg.GetTaskId() != "task-1" || req.Msg.GetInputResponse().GetMessage() != "please revise" {
				t.Fatalf("unexpected task reply: %#v", req.Msg)
			}
			return connect.NewResponse(&libopsv1.UpdateTaskResponse{Task: &libopsv1.Task{TaskId: "task-1"}}), nil
		},
	})

	if _, err := sendTaskReply(context.Background(), clients, "org-1", "task-1", "please revise", requestID); err != nil {
		t.Fatalf("sendTaskReply() error = %v", err)
	}
	if gotHeader != "cli-task-reply:"+requestID {
		t.Fatalf("Idempotency-Key = %q", gotHeader)
	}
}

func TestTaskCreateReleaseProfileFlags(t *testing.T) {
	tests := []struct {
		name        string
		wantDefault string
		wantUsage   string
	}{
		{name: "agent-model", wantDefault: "glm-5.2:cloud", wantUsage: "(glm-5.2:cloud)"},
		{name: "harness", wantDefault: "codex", wantUsage: "(codex)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := taskCreateCmd.Flags().Lookup(tt.name)
			if flag == nil {
				t.Fatalf("flag --%s is not registered", tt.name)
			}
			if flag.DefValue != tt.wantDefault {
				t.Errorf("--%s default = %q, want %q", tt.name, flag.DefValue, tt.wantDefault)
			}
			if !strings.Contains(flag.Usage, tt.wantUsage) {
				t.Errorf("--%s usage = %q, want containing %q", tt.name, flag.Usage, tt.wantUsage)
			}
		})
	}
}

func newTaskCreateTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().String("organization-id", "org-1", "")
	cmd.Flags().String("project-id", "project-1", "")
	cmd.Flags().String("site-id", "site-1", "")
	cmd.Flags().String("agent-model", "glm-5.2:cloud", "")
	cmd.Flags().String("harness", "codex", "")
	cmd.Flags().String("request-id", "", "")
	cmd.Flags().Bool("no-wait", false, "")
	cmd.Flags().Duration("poll-interval", time.Millisecond, "")
	return cmd
}

func newTestTaskAPIClients(t *testing.T, assistant libopsv1connect.AssistantServiceHandler, tasks libopsv1connect.TaskServiceHandler) *taskAPIClients {
	t.Helper()
	mux := http.NewServeMux()
	assistantPath, assistantHandler := libopsv1connect.NewAssistantServiceHandler(assistant)
	mux.Handle(assistantPath, assistantHandler)
	taskPath, taskHandler := libopsv1connect.NewTaskServiceHandler(tasks)
	mux.Handle(taskPath, taskHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &taskAPIClients{
		assistant: libopsv1connect.NewAssistantServiceClient(server.Client(), server.URL),
		tasks:     libopsv1connect.NewTaskServiceClient(server.Client(), server.URL),
	}
}

func unimplementedTestAssistantService() *testAssistantService {
	return &testAssistantService{
		chat: func(context.Context, *connect.Request[libopsv1.AssistantChatRequest]) (*connect.Response[libopsv1.AssistantChatResponse], error) {
			return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
		},
	}
}

func mustStruct(t *testing.T, values map[string]any) *structpb.Struct {
	t.Helper()
	value, err := structpb.NewStruct(values)
	if err != nil {
		t.Fatalf("structpb.NewStruct() error = %v", err)
	}
	return value
}
