package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	libopsv1 "github.com/libops/proto/libops/v1"
	"github.com/libops/proto/libops/v1/libopsv1connect"
	"github.com/libops/sitectl-libops/pkg/api"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

type taskAPIClients struct {
	assistant libopsv1connect.AssistantServiceClient
	tasks     libopsv1connect.TaskServiceClient
}

var supportedTaskAgentModels = map[string]struct{}{
	"glm-5.2:cloud": {},
}

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Create and manage LibOps Task Agent tasks",
}

var taskCreateCmd = &cobra.Command{
	Use:     "create <message>",
	Aliases: []string{"chat"},
	Short:   "Create a Task Agent task",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		clients, err := newTaskAPIClients(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}
		return runTaskCreate(cmd, args, clients)
	},
}

func runTaskCreate(cmd *cobra.Command, args []string, clients *taskAPIClients) error {
	orgID, _ := cmd.Flags().GetString("organization-id")
	projectID, _ := cmd.Flags().GetString("project-id")
	siteID, _ := cmd.Flags().GetString("site-id")
	agentModel, _ := cmd.Flags().GetString("agent-model")
	agentModel = strings.TrimSpace(agentModel)
	if agentModel != "" {
		if _, ok := supportedTaskAgentModels[agentModel]; !ok {
			return fmt.Errorf("unsupported agent model %q; Task Agent currently supports glm-5.2:cloud", agentModel)
		}
	}
	harnessRaw, _ := cmd.Flags().GetString("harness")
	harness, err := taskHarnessFromString(harnessRaw)
	if err != nil {
		return err
	}
	noWait, _ := cmd.Flags().GetBool("no-wait")
	pollInterval, _ := cmd.Flags().GetDuration("poll-interval")
	requestID, _ := cmd.Flags().GetString("request-id")
	requestID, err = normalizedTaskRequestID(requestID)
	if err != nil {
		return err
	}
	message := strings.TrimSpace(strings.Join(args, " "))

	resp, err := clients.assistant.Chat(cmd.Context(), connect.NewRequest(&libopsv1.AssistantChatRequest{
		OrganizationId:  orgID,
		ProjectId:       projectID,
		SiteId:          siteID,
		Message:         message,
		ClientRequestId: requestID,
		AgentModel:      agentModel,
		Harness:         harness,
		Metadata: map[string]string{
			"conversation_provider":        "cli",
			"conversation_response_target": "cli_poll",
		},
	}))
	if err != nil {
		return fmt.Errorf("failed to create task (retry with --request-id %s): %w", requestID, err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Created task: %s\n", resp.Msg.RequestId)
	if resp.Msg.Reply != "" {
		fmt.Fprintln(out, terminalSafe(resp.Msg.Reply))
	}
	if noWait {
		return nil
	}
	return runTaskChatSession(cmd.Context(), clients, orgID, resp.Msg.RequestId, pollInterval, cmd.InOrStdin(), out)
}

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Task Agent tasks",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		clients, err := newTaskAPIClients(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		orgID, _ := cmd.Flags().GetString("organization-id")
		projectID, _ := cmd.Flags().GetString("project-id")
		siteID, _ := cmd.Flags().GetString("site-id")
		limit, _ := cmd.Flags().GetInt32("limit")

		resp, err := clients.tasks.ListTasks(cmd.Context(), connect.NewRequest(&libopsv1.ListTasksRequest{
			OrganizationId: orgID,
			ProjectId:      projectID,
			SiteId:         siteID,
			Limit:          limit,
		}))
		if err != nil {
			return fmt.Errorf("failed to list tasks: %w", err)
		}

		if usage := resp.Msg.GetTaskUsage(); usage != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Tasks this month: %d/%d (%d remaining)\n\n", usage.GetUsed(), usage.GetLimit(), usage.GetRemaining())
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATUS\tSCOPE\tUPDATED\tPROMPT")
		for _, task := range resp.Msg.Tasks {
			scope := task.OrganizationId
			if task.ProjectId != "" {
				scope = task.ProjectId
			}
			if task.SiteId != "" {
				scope = task.SiteId
			}
			updated := ""
			if task.UpdatedAt != nil {
				updated = task.UpdatedAt.AsTime().Format("2006-01-02 15:04:05")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", singleLine(task.TaskId), task.Status.String(), singleLine(scope), updated, singleLine(task.Prompt))
		}
		return w.Flush()
	},
}

var taskGetCmd = &cobra.Command{
	Use:   "get <task-id>",
	Short: "Get a Task Agent task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		clients, err := newTaskAPIClients(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		orgID, _ := cmd.Flags().GetString("organization-id")
		resp, err := clients.tasks.GetTask(cmd.Context(), connect.NewRequest(&libopsv1.GetTaskRequest{
			OrganizationId: orgID,
			TaskId:         args[0],
		}))
		if err != nil {
			return fmt.Errorf("failed to get task: %w", err)
		}

		out, err := protojson.MarshalOptions{Indent: "  "}.Marshal(resp.Msg.Task)
		if err != nil {
			return fmt.Errorf("failed to marshal task: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), terminalSafe(string(out)))
		return nil
	},
}

var taskAttachCmd = &cobra.Command{
	Use:   "attach <task-id>",
	Short: "Attach to an existing Task Agent chat",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		clients, err := newTaskAPIClients(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}
		orgID, _ := cmd.Flags().GetString("organization-id")
		pollInterval, _ := cmd.Flags().GetDuration("poll-interval")
		return runTaskChatSession(cmd.Context(), clients, orgID, args[0], pollInterval, cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

var taskRespondCmd = &cobra.Command{
	Use:   "respond <task-id> <message>",
	Short: "Reply to a task that needs input",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		clients, err := newTaskAPIClients(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		orgID, _ := cmd.Flags().GetString("organization-id")
		noWait, _ := cmd.Flags().GetBool("no-wait")
		pollInterval, _ := cmd.Flags().GetDuration("poll-interval")
		requestID, _ := cmd.Flags().GetString("request-id")
		requestID, err = normalizedTaskRequestID(requestID)
		if err != nil {
			return err
		}
		taskID := args[0]
		message := strings.TrimSpace(strings.Join(args[1:], " "))

		resp, err := sendTaskReply(cmd.Context(), clients, orgID, taskID, message, requestID)
		if err != nil {
			return fmt.Errorf("failed to reply to task (retry with --request-id %s): %w", requestID, err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Updated task: %s\n", resp.Msg.GetTask().GetTaskId())
		if noWait {
			return nil
		}
		return runTaskChatSession(cmd.Context(), clients, orgID, taskID, pollInterval, cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

var taskCancelCmd = &cobra.Command{
	Use:   "cancel <task-id>",
	Short: "Cancel a Task Agent task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		clients, err := newTaskAPIClients(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		orgID, _ := cmd.Flags().GetString("organization-id")
		resp, err := clients.tasks.CancelTask(cmd.Context(), connect.NewRequest(&libopsv1.CancelTaskRequest{
			OrganizationId: orgID,
			TaskId:         args[0],
		}))
		if err != nil {
			return fmt.Errorf("failed to cancel task: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Canceled task: %s\n", resp.Msg.GetTask().GetTaskId())
		return nil
	},
}

func init() {
	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
	taskCmd.AddCommand(taskAttachCmd)
	taskCmd.AddCommand(taskRespondCmd)
	taskCmd.AddCommand(taskCancelCmd)

	for _, c := range []*cobra.Command{taskCreateCmd, taskListCmd, taskGetCmd, taskAttachCmd, taskRespondCmd, taskCancelCmd} {
		c.Flags().String("organization-id", "", "Organization ID")
		_ = c.MarkFlagRequired("organization-id")
	}

	taskCreateCmd.Flags().String("project-id", "", "Project ID")
	taskCreateCmd.Flags().String("site-id", "", "Site ID")
	taskCreateCmd.Flags().String("agent-model", "glm-5.2:cloud", "Coding agent model (glm-5.2:cloud)")
	taskCreateCmd.Flags().String("harness", "codex", "Coding harness (codex)")
	taskCreateCmd.Flags().String("request-id", "", "Stable UUID for safely retrying task creation")
	taskCreateCmd.Flags().Bool("no-wait", false, "Return after queueing the task")
	taskCreateCmd.Flags().Duration("poll-interval", 3*time.Second, "Task polling interval while attached")
	taskAttachCmd.Flags().Duration("poll-interval", 3*time.Second, "Task polling interval while attached")
	taskListCmd.Flags().String("project-id", "", "Filter by project ID")
	taskListCmd.Flags().String("site-id", "", "Filter by site ID")
	taskListCmd.Flags().Int32("limit", 50, "Maximum tasks to return")
	taskRespondCmd.Flags().Bool("no-wait", false, "Return after queueing the reply")
	taskRespondCmd.Flags().Duration("poll-interval", 3*time.Second, "Task polling interval while attached")
	taskRespondCmd.Flags().String("request-id", "", "Stable UUID for safely retrying this task reply")
}

func newTaskAPIClients(ctx context.Context, apiBaseURL string) (*taskAPIClients, error) {
	httpClient, err := api.NewAuthenticatedHTTPClient(ctx, apiBaseURL)
	if err != nil {
		return nil, err
	}
	return &taskAPIClients{
		assistant: libopsv1connect.NewAssistantServiceClient(httpClient, apiBaseURL),
		tasks:     libopsv1connect.NewTaskServiceClient(httpClient, apiBaseURL),
	}, nil
}

func taskHarnessFromString(raw string) (libopsv1.TaskHarness, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "codex":
		return libopsv1.TaskHarness_TASK_HARNESS_CODEX, nil
	default:
		return libopsv1.TaskHarness_TASK_HARNESS_UNSPECIFIED, fmt.Errorf("unsupported harness %q; Task Agent currently supports codex", raw)
	}
}

func normalizedTaskRequestID(raw string) (string, error) {
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

func sendTaskReply(ctx context.Context, clients *taskAPIClients, orgID, taskID, message, requestID string) (*connect.Response[libopsv1.UpdateTaskResponse], error) {
	request := connect.NewRequest(&libopsv1.UpdateTaskRequest{
		OrganizationId: orgID,
		TaskId:         taskID,
		Status:         libopsv1.TaskStatus_TASK_STATUS_QUEUED,
		Prompt:         message,
		InputResponse: &libopsv1.TaskInput{
			Message: message,
		},
	})
	request.Header().Set("Idempotency-Key", "cli-task-reply:"+requestID)
	return clients.tasks.UpdateTask(ctx, request)
}

func singleLine(value string) string {
	value = terminalSafe(value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 96 {
		return value[:93] + "..."
	}
	return value
}

func runTaskChatSession(ctx context.Context, clients *taskAPIClients, orgID, taskID string, interval time.Duration, in io.Reader, out io.Writer) error {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	reader := bufio.NewReader(in)
	consecutivePollErrors := 0
	var lastStatus libopsv1.TaskStatus
	var lastMessageCount int
	var lastResultCount int
	for {
		resp, err := clients.tasks.GetTask(ctx, connect.NewRequest(&libopsv1.GetTaskRequest{
			OrganizationId: orgID,
			TaskId:         taskID,
		}))
		if err != nil {
			if isRetryableTaskPollError(err) && consecutivePollErrors < 2 {
				consecutivePollErrors++
				if waitErr := waitForTaskPoll(ctx, interval); waitErr != nil {
					return waitErr
				}
				continue
			}
			return fmt.Errorf("failed to get task: %w", err)
		}
		consecutivePollErrors = 0
		task := resp.Msg.GetTask()
		if task == nil {
			return fmt.Errorf("task not found")
		}
		if task.GetStatus() != lastStatus || len(task.GetMessages()) != lastMessageCount || len(task.GetResults()) != lastResultCount {
			printTaskChatUpdate(out, task)
			lastStatus = task.GetStatus()
			lastMessageCount = len(task.GetMessages())
			lastResultCount = len(task.GetResults())
		}
		switch task.GetStatus() {
		case libopsv1.TaskStatus_TASK_STATUS_COMPLETED:
			return nil
		case libopsv1.TaskStatus_TASK_STATUS_FAILED:
			if reason := taskFailureReason(task); reason != "" {
				return fmt.Errorf("task %s failed: %s", task.GetTaskId(), reason)
			}
			return fmt.Errorf("task %s failed", task.GetTaskId())
		case libopsv1.TaskStatus_TASK_STATUS_CANCELED:
			return fmt.Errorf("task %s was canceled", task.GetTaskId())
		}
		if taskAPIAction(task) != nil {
			return nil
		}
		if task.GetStatus() == libopsv1.TaskStatus_TASK_STATUS_RUNNING && taskPullRequestReady(task) {
			return nil
		}
		switch task.GetStatus() {
		case libopsv1.TaskStatus_TASK_STATUS_NEEDS_INPUT:
			fmt.Fprint(out, "reply> ")
			line, err := reader.ReadString('\n')
			// Readers may return the final bytes together with io.EOF. Process
			// those bytes so non-interactive callers do not need a trailing
			// newline for their reply to be delivered.
			if err != nil && len(line) == 0 {
				return err
			}
			message := strings.TrimSpace(line)
			if message == "" {
				continue
			}
			requestID := uuid.NewString()
			_, err = sendTaskReply(ctx, clients, orgID, taskID, message, requestID)
			if err != nil {
				return fmt.Errorf("failed to reply to task (retry with `sitectl task respond %s <message> --request-id %s`): %w", taskID, requestID, err)
			}
			fmt.Fprintln(out, "Reply sent.")
		default:
			if err := waitForTaskPoll(ctx, interval); err != nil {
				return err
			}
		}
	}
}

func taskPullRequestReady(task *libopsv1.Task) bool {
	return currentTaskPullRequestResult(task) != nil
}

func currentTaskPullRequestResult(task *libopsv1.Task) *libopsv1.TaskResult {
	if task == nil {
		return nil
	}
	followupGeneration, valid := taskStructUint64(task.GetInputResponse().GetFields(), "task_followup_generation")
	if !valid {
		return nil
	}
	for _, result := range task.GetResults() {
		if result.GetType() != libopsv1.TaskResultType_TASK_RESULT_PR_CREATED || strings.TrimSpace(result.GetPrUrl()) == "" {
			continue
		}
		resultGeneration, valid := taskStructUint64(result.GetMetadata(), "task_followup_generation")
		if valid && resultGeneration >= followupGeneration {
			return result
		}
	}
	return nil
}

func taskStructUint64(value *structpb.Struct, key string) (uint64, bool) {
	if value == nil {
		return 0, true
	}
	rawValue, exists := value.AsMap()[key]
	if !exists {
		return 0, true
	}
	raw := strings.TrimSpace(fmt.Sprint(rawValue))
	parsed, err := strconv.ParseUint(raw, 10, 64)
	return parsed, err == nil
}

func isRetryableTaskPollError(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeAborted, connect.CodeDeadlineExceeded, connect.CodeResourceExhausted, connect.CodeUnavailable:
		return true
	default:
		return false
	}
}

func waitForTaskPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func printTaskChatUpdate(out io.Writer, task *libopsv1.Task) {
	if task.GetStatus() == libopsv1.TaskStatus_TASK_STATUS_COMPLETED {
		printTaskCompletion(out, task)
		return
	}
	if task.GetStatus() == libopsv1.TaskStatus_TASK_STATUS_RUNNING {
		if result := currentTaskPullRequestResult(task); result != nil {
			printTaskReviewReady(out, task, result)
			return
		}
	}

	fmt.Fprintf(out, "\n[%s] %s\n", task.GetStatus().String(), task.GetTaskId())
	if message := task.GetInputRequest().GetMessage(); task.GetStatus() == libopsv1.TaskStatus_TASK_STATUS_NEEDS_INPUT && strings.TrimSpace(message) != "" {
		fmt.Fprintln(out, terminalSafe(message))
	}
	for _, result := range task.GetResults() {
		switch result.GetType() {
		case libopsv1.TaskResultType_TASK_RESULT_PR_CREATED:
			if result.GetPrUrl() != "" {
				fmt.Fprintf(out, "Pull request: %s\n", singleLine(result.GetPrUrl()))
			}
		case libopsv1.TaskResultType_TASK_RESULT_DEPLOYMENT:
			if result.GetDeploymentId() != "" {
				fmt.Fprintf(out, "Deployment: %s\n", singleLine(result.GetDeploymentId()))
			}
		case libopsv1.TaskResultType_TASK_RESULT_API_ACTION:
			if action := result.GetApiAction(); action != nil {
				printTaskAPIAction(out, action)
			}
		}
	}
}

func printTaskReviewReady(out io.Writer, task *libopsv1.Task, result *libopsv1.TaskResult) {
	fmt.Fprintf(out, "\nLibOps task `%s` is ready for review.\n", singleLine(shortTaskID(task.GetTaskId())))
	fmt.Fprintf(out, "Pull request: %s\n", singleLine(result.GetPrUrl()))
	previewURL := firstNonEmptyString(
		taskResultMetadataString(result, "preview_url"),
		taskResultMetadataString(result, "preview_site_url"),
		taskInputFieldString(task.GetInputResponse(), "preview_url"),
		taskInputFieldString(task.GetInputResponse(), "preview_site_url"),
	)
	if previewURL != "" {
		fmt.Fprintf(out, "Preview: %s\n", singleLine(previewURL))
	}
	if summary := taskResultMetadataString(result, "summary"); summary != "" {
		fmt.Fprintf(out, "Summary:\n%s\n", terminalSafe(summary))
	}
	fmt.Fprintln(out, "The task remains open until this pull request is merged or closed.")
}

func printTaskCompletion(out io.Writer, task *libopsv1.Task) {
	fmt.Fprintf(out, "\nLibOps task `%s` is ready.\n", singleLine(shortTaskID(task.GetTaskId())))
	var previewURL, prURL, summary string
	for _, result := range task.GetResults() {
		if result.GetDeploymentId() != "" {
			fmt.Fprintf(out, "Deployment: `%s`\n", singleLine(result.GetDeploymentId()))
		}
		if action := result.GetApiAction(); action != nil {
			printTaskAPIAction(out, action)
		}
		if previewURL == "" {
			previewURL = firstNonEmptyString(
				taskResultMetadataString(result, "preview_url"),
				taskResultMetadataString(result, "preview_site_url"),
			)
		}
		if prURL == "" {
			prURL = strings.TrimSpace(result.GetPrUrl())
		}
		if summary == "" {
			summary = taskResultMetadataString(result, "summary")
		}
	}
	if previewURL != "" {
		fmt.Fprintf(out, "Preview: %s\n", singleLine(previewURL))
	}
	if prURL != "" {
		fmt.Fprintf(out, "Pull request: %s\n", singleLine(prURL))
	}
	if summary != "" {
		fmt.Fprintf(out, "Summary:\n%s\n", terminalSafe(summary))
	}
}

func taskAPIAction(task *libopsv1.Task) *libopsv1.TaskApiAction {
	for _, result := range task.GetResults() {
		if result.GetType() != libopsv1.TaskResultType_TASK_RESULT_API_ACTION {
			continue
		}
		if action := result.GetApiAction(); action != nil {
			return action
		}
	}
	return nil
}

func taskFailureReason(task *libopsv1.Task) string {
	logs := task.GetLogs()
	for i := len(logs) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(logs[i].GetLevel()), "error") {
			if message := strings.TrimSpace(logs[i].GetMessage()); message != "" {
				return terminalSafe(message)
			}
		}
	}
	return ""
}

func printTaskAPIAction(out io.Writer, action *libopsv1.TaskApiAction) {
	fmt.Fprintln(out, "API action ready. Run this request with an authenticated LibOps API client:")
	if description := strings.TrimSpace(action.GetDescription()); description != "" {
		fmt.Fprintf(out, "Description: %s\n", terminalSafe(description))
	}
	fmt.Fprintf(out, "Method: %s\n", singleLine(strings.ToUpper(strings.TrimSpace(action.GetMethod()))))
	fmt.Fprintf(out, "Path: %s\n", singleLine(action.GetPath()))

	headers, omitted := safeAPIActionHeaders(action)
	fmt.Fprintln(out, "Headers:")
	fmt.Fprintln(out, prettyJSON(headers))
	if omitted > 0 {
		fmt.Fprintf(out, "%d secret-bearing header(s) omitted.\n", omitted)
	}
	fmt.Fprintln(out, "Body:")
	body := map[string]any{}
	if action.GetBody() != nil {
		body = action.GetBody().AsMap()
	}
	fmt.Fprintln(out, prettyJSON(body))
	fmt.Fprintln(out, "Authentication is supplied by your API client; credential values are never displayed.")
}

func safeAPIActionHeaders(action *libopsv1.TaskApiAction) (map[string]any, int) {
	safe := map[string]any{}
	if action.GetHeaders() == nil {
		return safe, 0
	}
	omitted := 0
	for name, value := range action.GetHeaders().AsMap() {
		if isSensitiveHeader(name) {
			omitted++
			continue
		}
		safe[name] = value
	}
	return safe, omitted
}

func isSensitiveHeader(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.NewReplacer("_", "-", " ", "-").Replace(normalized)
	for _, marker := range []string{
		"authorization",
		"cookie",
		"token",
		"secret",
		"password",
		"credential",
		"api-key",
		"apikey",
		"signature",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func prettyJSON(value any) string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return terminalSafe(string(encoded))
}

// terminalSafe removes terminal control and Unicode formatting characters from
// server/model-controlled output. Newlines and tabs remain available for the
// intentionally formatted task transcript and JSON instructions.
func terminalSafe(value string) string {
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' {
			return character
		}
		if unicode.IsControl(character) || unicode.In(character, unicode.Cf) {
			return -1
		}
		return character
	}, value)
}

func taskResultMetadataString(result *libopsv1.TaskResult, key string) string {
	if result == nil || result.GetMetadata() == nil {
		return ""
	}
	value, _ := result.GetMetadata().AsMap()[key].(string)
	return strings.TrimSpace(value)
}

func taskInputFieldString(input *libopsv1.TaskInput, key string) string {
	if input == nil || input.GetFields() == nil {
		return ""
	}
	value, _ := input.GetFields().AsMap()[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func shortTaskID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if len(taskID) <= 8 {
		return taskID
	}
	return taskID[:8]
}
