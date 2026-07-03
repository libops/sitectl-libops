package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"connectrpc.com/connect"
	libopsv1 "github.com/libops/proto/libops/v1"
	"github.com/libops/proto/libops/v1/libopsv1connect"
	"github.com/libops/sitectl-libops/pkg/api"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

type taskAPIClients struct {
	assistant libopsv1connect.AssistantServiceClient
	tasks     libopsv1connect.TaskServiceClient
}

var supportedTaskAgentModels = map[string]struct{}{
	"glm-5.2:cloud": {},
	"kimi-k2.6":     {},
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

		orgID, _ := cmd.Flags().GetString("organization-id")
		projectID, _ := cmd.Flags().GetString("project-id")
		siteID, _ := cmd.Flags().GetString("site-id")
		agentModel, _ := cmd.Flags().GetString("agent-model")
		agentModel = strings.TrimSpace(agentModel)
		if agentModel != "" {
			if _, ok := supportedTaskAgentModels[agentModel]; !ok {
				return fmt.Errorf("unsupported agent model %q; Task Agent supports glm-5.2:cloud, kimi-k2.6", agentModel)
			}
		}
		harnessRaw, _ := cmd.Flags().GetString("harness")
		harness, err := taskHarnessFromString(harnessRaw)
		if err != nil {
			return err
		}
		noWait, _ := cmd.Flags().GetBool("no-wait")
		pollInterval, _ := cmd.Flags().GetDuration("poll-interval")
		message := strings.TrimSpace(strings.Join(args, " "))

		resp, err := clients.assistant.Chat(cmd.Context(), connect.NewRequest(&libopsv1.AssistantChatRequest{
			OrganizationId: orgID,
			ProjectId:      projectID,
			SiteId:         siteID,
			Message:        message,
			AgentModel:     agentModel,
			Harness:        harness,
			Metadata: map[string]string{
				"conversation_provider":        "cli",
				"conversation_response_target": "cli_poll",
			},
		}))
		if err != nil {
			return fmt.Errorf("failed to create task: %w", err)
		}

		fmt.Printf("Created task: %s\n", resp.Msg.RequestId)
		if resp.Msg.Reply != "" {
			fmt.Println(resp.Msg.Reply)
		}
		if noWait {
			return nil
		}
		return runTaskChatSession(cmd.Context(), clients, orgID, resp.Msg.RequestId, pollInterval)
	},
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
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", task.TaskId, task.Status.String(), scope, updated, singleLine(task.Prompt))
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
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
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
		return runTaskChatSession(cmd.Context(), clients, orgID, args[0], pollInterval)
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
		taskID := args[0]
		message := strings.TrimSpace(strings.Join(args[1:], " "))

		resp, err := clients.tasks.UpdateTask(cmd.Context(), connect.NewRequest(&libopsv1.UpdateTaskRequest{
			OrganizationId: orgID,
			TaskId:         taskID,
			Status:         libopsv1.TaskStatus_TASK_STATUS_QUEUED,
			Prompt:         message,
			InputResponse: &libopsv1.TaskInput{
				Message: message,
			},
		}))
		if err != nil {
			return fmt.Errorf("failed to reply to task: %w", err)
		}

		fmt.Printf("Updated task: %s\n", resp.Msg.GetTask().GetTaskId())
		if noWait {
			return nil
		}
		return runTaskChatSession(cmd.Context(), clients, orgID, taskID, pollInterval)
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

		fmt.Printf("Canceled task: %s\n", resp.Msg.GetTask().GetTaskId())
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
	taskCreateCmd.Flags().String("agent-model", "glm-5.2:cloud", "Coding agent model (glm-5.2:cloud, kimi-k2.6)")
	taskCreateCmd.Flags().String("harness", "codex", "Coding harness (codex, claude, pi, opencode, gemini)")
	taskCreateCmd.Flags().Bool("no-wait", false, "Return after queueing the task")
	taskCreateCmd.Flags().Duration("poll-interval", 3*time.Second, "Task polling interval while attached")
	taskAttachCmd.Flags().Duration("poll-interval", 3*time.Second, "Task polling interval while attached")
	taskListCmd.Flags().String("project-id", "", "Filter by project ID")
	taskListCmd.Flags().String("site-id", "", "Filter by site ID")
	taskListCmd.Flags().Int32("limit", 50, "Maximum tasks to return")
	taskRespondCmd.Flags().Bool("no-wait", false, "Return after queueing the reply")
	taskRespondCmd.Flags().Duration("poll-interval", 3*time.Second, "Task polling interval while attached")
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
	case "claude":
		return libopsv1.TaskHarness_TASK_HARNESS_CLAUDE, nil
	case "pi":
		return libopsv1.TaskHarness_TASK_HARNESS_PI, nil
	case "opencode":
		return libopsv1.TaskHarness_TASK_HARNESS_OPENCODE, nil
	case "gemini":
		return libopsv1.TaskHarness_TASK_HARNESS_GEMINI, nil
	default:
		return libopsv1.TaskHarness_TASK_HARNESS_UNSPECIFIED, fmt.Errorf("unsupported harness %q; Task Agent supports codex, claude, pi, opencode, gemini", raw)
	}
}

func singleLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 96 {
		return value[:93] + "..."
	}
	return value
}

func runTaskChatSession(ctx context.Context, clients *taskAPIClients, orgID, taskID string, interval time.Duration) error {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	reader := bufio.NewReader(os.Stdin)
	var lastStatus libopsv1.TaskStatus
	var lastMessageCount int
	var lastResultCount int
	for {
		resp, err := clients.tasks.GetTask(ctx, connect.NewRequest(&libopsv1.GetTaskRequest{
			OrganizationId: orgID,
			TaskId:         taskID,
		}))
		if err != nil {
			return fmt.Errorf("failed to get task: %w", err)
		}
		task := resp.Msg.GetTask()
		if task == nil {
			return fmt.Errorf("task not found")
		}
		if task.GetStatus() != lastStatus || len(task.GetMessages()) != lastMessageCount || len(task.GetResults()) != lastResultCount {
			printTaskChatUpdate(task)
			lastStatus = task.GetStatus()
			lastMessageCount = len(task.GetMessages())
			lastResultCount = len(task.GetResults())
		}
		switch task.GetStatus() {
		case libopsv1.TaskStatus_TASK_STATUS_COMPLETED, libopsv1.TaskStatus_TASK_STATUS_FAILED, libopsv1.TaskStatus_TASK_STATUS_CANCELED:
			return nil
		case libopsv1.TaskStatus_TASK_STATUS_NEEDS_INPUT:
			fmt.Print("reply> ")
			line, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			message := strings.TrimSpace(line)
			if message == "" {
				continue
			}
			_, err = clients.tasks.UpdateTask(ctx, connect.NewRequest(&libopsv1.UpdateTaskRequest{
				OrganizationId: orgID,
				TaskId:         taskID,
				Status:         libopsv1.TaskStatus_TASK_STATUS_QUEUED,
				Prompt:         message,
				InputResponse: &libopsv1.TaskInput{
					Message: message,
				},
			}))
			if err != nil {
				return fmt.Errorf("failed to reply to task: %w", err)
			}
			fmt.Println("Reply sent.")
		default:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}
		}
	}
}

func printTaskChatUpdate(task *libopsv1.Task) {
	if task.GetStatus() == libopsv1.TaskStatus_TASK_STATUS_COMPLETED {
		printTaskCompletion(task)
		return
	}

	fmt.Printf("\n[%s] %s\n", task.GetStatus().String(), task.GetTaskId())
	if message := task.GetInputRequest().GetMessage(); task.GetStatus() == libopsv1.TaskStatus_TASK_STATUS_NEEDS_INPUT && strings.TrimSpace(message) != "" {
		fmt.Println(message)
	}
	for _, result := range task.GetResults() {
		switch result.GetType() {
		case libopsv1.TaskResultType_TASK_RESULT_PR_CREATED:
			if result.GetPrUrl() != "" {
				fmt.Printf("Pull request: %s\n", result.GetPrUrl())
			}
		case libopsv1.TaskResultType_TASK_RESULT_DEPLOYMENT:
			if result.GetDeploymentId() != "" {
				fmt.Printf("Deployment: %s\n", result.GetDeploymentId())
			}
		case libopsv1.TaskResultType_TASK_RESULT_API_ACTION:
			if action := result.GetApiAction(); action != nil {
				fmt.Printf("Action ready: %s %s\n", strings.ToUpper(action.GetMethod()), action.GetPath())
				if action.GetDescription() != "" {
					fmt.Println(action.GetDescription())
				}
			}
		}
	}
}

func printTaskCompletion(task *libopsv1.Task) {
	fmt.Printf("\nLibOps task `%s` is ready.\n", shortTaskID(task.GetTaskId()))
	var previewURL, prURL, summary string
	for _, result := range task.GetResults() {
		if result.GetDeploymentId() != "" {
			fmt.Printf("Deployment: `%s`\n", result.GetDeploymentId())
		}
		if action := result.GetApiAction(); action != nil {
			fmt.Printf("Action ready: %s %s\n", strings.ToUpper(action.GetMethod()), action.GetPath())
			if action.GetDescription() != "" {
				fmt.Println(action.GetDescription())
			}
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
		fmt.Printf("Preview: %s\n", previewURL)
	}
	if prURL != "" {
		fmt.Printf("Pull request: %s\n", prURL)
	}
	if summary != "" {
		fmt.Printf("Summary:\n%s\n", summary)
	}
}

func taskResultMetadataString(result *libopsv1.TaskResult, key string) string {
	if result == nil || result.GetMetadata() == nil {
		return ""
	}
	value, _ := result.GetMetadata().AsMap()[key].(string)
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
