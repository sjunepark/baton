package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/repository"
	"github.com/sjunepark/baton/internal/task"
)

const (
	exitOK          = 0
	exitOperational = 1
	exitUsage       = 2

	githubRequestTimeout        = 30 * time.Second
	githubResponseHeaderTimeout = 15 * time.Second
	githubConnectTimeout        = 10 * time.Second
)

var commandOrder = []string{"list", "show", "next", "enroll", "update", "unenroll", "start", "stop", "close"}

type runtime struct {
	getenv     func(string) string
	resolve    func(context.Context, repository.TaskOptions) (string, error)
	newService func(context.Context) (*task.Service, error)
}

func defaultRuntime() runtime {
	getenv := os.Getenv
	return runtime{
		getenv:  getenv,
		resolve: repository.ResolveTaskRepositoryContext,
		newService: func(ctx context.Context) (*task.Service, error) {
			credentials, err := auth.DiscoverContext(ctx, auth.Inputs{
				GitHubToken: getenv("GITHUB_TOKEN"), GHToken: getenv("GH_TOKEN"),
			})
			if err != nil {
				return nil, err
			}
			client := gh.NewClientWithCredentials(getenv("GITHUB_API_URL"), credentials, newProductionHTTPClient())
			return task.NewService(task.NewGitHubStore(client)), nil
		},
	}
}

func newProductionHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: githubConnectTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = githubResponseHeaderTimeout
	return &http.Client{Transport: transport, Timeout: githubRequestTimeout}
}

func Run(args []string, stdout, stderr io.Writer, version string) int {
	return RunContext(context.Background(), args, stdout, stderr, version)
}

func RunContext(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	return runContext(ctx, args, stdout, stderr, version, defaultRuntime())
}

type rootInput struct {
	repository string
	json       bool
	command    string
	args       []string
	action     string
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer, version string, rt runtime) int {
	root, err := parseRoot(args)
	out := renderer{stdout: stdout, stderr: stderr, json: root.json}
	if err != nil {
		return out.usage(err.Error(), "Run `baton --help` for valid syntax.")
	}
	switch root.action {
	case "help":
		if err := printHelp(stdout); err != nil {
			return out.outputError(err)
		}
		return exitOK
	case "version":
		if _, err := fmt.Fprintln(stdout, version); err != nil {
			return out.outputError(err)
		}
		return exitOK
	}
	if _, valid := commandHelp[root.command]; !valid {
		return out.usage(fmt.Sprintf("unknown command %q", root.command), "Valid commands: "+strings.Join(commandOrder, ", ")+".")
	}
	if len(root.args) == 1 && (root.args[0] == "--help" || root.args[0] == "-h") {
		if err := printCommandHelp(stdout, root.command); err != nil {
			return out.outputError(err)
		}
		return exitOK
	}
	request, err := parseCommand(root.command, root.args)
	if err != nil {
		return out.usage(err.Error(), fmt.Sprintf("Run `baton %s --help` for valid syntax.", root.command))
	}
	resolveOptions := repository.TaskOptions{Repository: root.repository}
	if root.repository == "" {
		resolveOptions.EnvironmentRepo = rt.getenv("GITHUB_REPOSITORY")
		if resolveOptions.EnvironmentRepo == "" {
			resolveOptions.GitHubAPIURL = rt.getenv("GITHUB_API_URL")
		}
	}
	repositoryName, err := rt.resolve(ctx, resolveOptions)
	if err != nil {
		var repositoryErr *repository.TaskResolveError
		if errors.As(err, &repositoryErr) && repositoryErr.Usage {
			return out.usage(repositoryErr.Message, repositoryErr.Hint)
		}
		return out.operational(err)
	}
	service, err := rt.newService(ctx)
	if err != nil {
		return out.operational(err)
	}
	return execute(ctx, out, service, repositoryName, root.command, request)
}

func parseRoot(args []string) (rootInput, error) {
	if len(args) == 0 {
		return rootInput{action: "help"}, nil
	}
	root := rootInput{}
	for index := 0; index < len(args); index++ {
		value := args[index]
		switch {
		case value == "--help" || value == "-h":
			if index != len(args)-1 || root.repository != "" || root.json {
				return root, fmt.Errorf("--help cannot be combined with other arguments")
			}
			root.action = "help"
			return root, nil
		case value == "--version":
			if index != len(args)-1 || root.repository != "" || root.json {
				return root, fmt.Errorf("--version accepts no other arguments")
			}
			root.action = "version"
			return root, nil
		case value == "--json":
			if root.json {
				return root, fmt.Errorf("--json may be specified only once")
			}
			root.json = true
		case value == "--repo":
			if root.repository != "" || index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
				return root, fmt.Errorf("--repo requires one owner/name value")
			}
			index++
			root.repository = args[index]
		case strings.HasPrefix(value, "--repo="):
			if root.repository != "" || strings.TrimPrefix(value, "--repo=") == "" {
				return root, fmt.Errorf("--repo requires one owner/name value")
			}
			root.repository = strings.TrimPrefix(value, "--repo=")
		case strings.HasPrefix(value, "-"):
			return root, fmt.Errorf("unknown global flag %q", value)
		default:
			root.command = value
			root.args = args[index+1:]
			return root, nil
		}
	}
	return root, fmt.Errorf("a command is required")
}

type commandRequest struct {
	state    task.ListState
	number   int
	full     bool
	dryRun   bool
	mutation task.Mutation
}

type flagKind int

const (
	boolFlag flagKind = iota
	valueFlag
	repeatFlag
)

type scanned struct {
	positionals []string
	bools       map[string]bool
	values      map[string][]string
}

func scanCommandArgs(args []string, allowed map[string]flagKind) (scanned, error) {
	result := scanned{bools: map[string]bool{}, values: map[string][]string{}}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "--") {
			if strings.HasPrefix(argument, "-") {
				return result, fmt.Errorf("unknown flag %q", argument)
			}
			result.positionals = append(result.positionals, argument)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(argument, "--"), "=")
		kind, ok := allowed[name]
		if !ok {
			return result, fmt.Errorf("unknown flag --%s", name)
		}
		if kind == boolFlag {
			if hasValue || result.bools[name] {
				return result, fmt.Errorf("--%s is a boolean flag and may be specified only once", name)
			}
			result.bools[name] = true
			continue
		}
		if !hasValue {
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return result, fmt.Errorf("--%s requires a value", name)
			}
			index++
			value = args[index]
		}
		if kind == valueFlag && len(result.values[name]) > 0 {
			return result, fmt.Errorf("--%s may be specified only once", name)
		}
		result.values[name] = append(result.values[name], value)
	}
	return result, nil
}

func parseCommand(command string, args []string) (commandRequest, error) {
	switch command {
	case "list":
		parsed, err := scanCommandArgs(args, map[string]flagKind{"state": valueFlag})
		if err != nil {
			return commandRequest{}, err
		}
		if len(parsed.positionals) != 0 {
			return commandRequest{}, fmt.Errorf("list accepts no arguments")
		}
		state := task.ListOpen
		if values := parsed.values["state"]; len(values) > 0 {
			state = task.ListState(values[0])
			if state != task.ListOpen && state != task.ListClosed && state != task.ListAll {
				return commandRequest{}, fmt.Errorf("--state must be open, closed, or all")
			}
		}
		return commandRequest{state: state}, nil
	case "show":
		parsed, err := scanCommandArgs(args, map[string]flagKind{"full": boolFlag})
		if err != nil {
			return commandRequest{}, err
		}
		number, err := oneIssueNumber("show", parsed.positionals)
		return commandRequest{number: number, full: parsed.bools["full"]}, err
	case "next":
		parsed, err := scanCommandArgs(args, nil)
		if err != nil {
			return commandRequest{}, err
		}
		if len(parsed.positionals) != 0 {
			return commandRequest{}, fmt.Errorf("next accepts no arguments")
		}
		return commandRequest{}, nil
	case "enroll":
		return parseEnroll(args)
	case "update":
		return parseUpdate(args)
	case "unenroll", "start", "stop", "close":
		parsed, err := scanCommandArgs(args, map[string]flagKind{"dry-run": boolFlag})
		if err != nil {
			return commandRequest{}, err
		}
		number, err := oneIssueNumber(command, parsed.positionals)
		if err != nil {
			return commandRequest{}, err
		}
		return commandRequest{number: number, dryRun: parsed.bools["dry-run"], mutation: task.Mutation{Kind: task.MutationKind(command)}}, nil
	default:
		return commandRequest{}, fmt.Errorf("unknown command %q", command)
	}
}

func parseEnroll(args []string) (commandRequest, error) {
	parsed, err := scanCommandArgs(args, map[string]flagKind{"mode": valueFlag, "priority": valueFlag, "dry-run": boolFlag})
	if err != nil {
		return commandRequest{}, err
	}
	number, err := oneIssueNumber("enroll", parsed.positionals)
	if err != nil {
		return commandRequest{}, err
	}
	mutation := task.Mutation{Kind: task.MutationEnroll}
	if values := parsed.values["mode"]; len(values) > 0 {
		mode, parseErr := parseMode(values[0], false)
		if parseErr != nil {
			return commandRequest{}, parseErr
		}
		mutation.ModeSet, mutation.Mode = true, mode
	}
	if values := parsed.values["priority"]; len(values) > 0 {
		priority, parseErr := parsePriority(values[0], false)
		if parseErr != nil {
			return commandRequest{}, parseErr
		}
		mutation.PrioritySet, mutation.Priority = true, priority
	}
	return commandRequest{number: number, dryRun: parsed.bools["dry-run"], mutation: mutation}, nil
}

func parseUpdate(args []string) (commandRequest, error) {
	parsed, err := scanCommandArgs(args, map[string]flagKind{
		"mode": valueFlag, "priority": valueFlag, "add-blocker": repeatFlag,
		"remove-blocker": repeatFlag, "dry-run": boolFlag,
	})
	if err != nil {
		return commandRequest{}, err
	}
	number, err := oneIssueNumber("update", parsed.positionals)
	if err != nil {
		return commandRequest{}, err
	}
	mutation := task.Mutation{Kind: task.MutationUpdate}
	if values := parsed.values["mode"]; len(values) > 0 {
		mode, parseErr := parseMode(values[0], true)
		if parseErr != nil {
			return commandRequest{}, parseErr
		}
		mutation.ModeSet, mutation.Mode = true, mode
	}
	if values := parsed.values["priority"]; len(values) > 0 {
		priority, parseErr := parsePriority(values[0], true)
		if parseErr != nil {
			return commandRequest{}, parseErr
		}
		mutation.PrioritySet, mutation.Priority = true, priority
	}
	for _, blocker := range parsed.values["add-blocker"] {
		if !validBlocker(blocker) {
			return commandRequest{}, fmt.Errorf("--add-blocker must be needs-info or needs:discussion")
		}
		mutation.AddBlockers = append(mutation.AddBlockers, strings.ToLower(blocker))
	}
	removed := map[string]struct{}{}
	for _, blocker := range parsed.values["remove-blocker"] {
		if !validBlocker(blocker) {
			return commandRequest{}, fmt.Errorf("--remove-blocker must be needs-info or needs:discussion")
		}
		blocker = strings.ToLower(blocker)
		removed[blocker] = struct{}{}
		mutation.RemoveBlockers = append(mutation.RemoveBlockers, blocker)
	}
	for _, blocker := range mutation.AddBlockers {
		if _, conflict := removed[blocker]; conflict {
			return commandRequest{}, fmt.Errorf("blocker %q cannot be added and removed together", blocker)
		}
	}
	if !mutation.ModeSet && !mutation.PrioritySet && len(mutation.AddBlockers) == 0 && len(mutation.RemoveBlockers) == 0 {
		return commandRequest{}, fmt.Errorf("update requires at least one change flag")
	}
	return commandRequest{number: number, dryRun: parsed.bools["dry-run"], mutation: mutation}, nil
}

func oneIssueNumber(command string, positionals []string) (int, error) {
	if len(positionals) != 1 {
		return 0, fmt.Errorf("%s requires one positive issue number", command)
	}
	number, err := strconv.Atoi(positionals[0])
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("issue number must be a positive integer")
	}
	return number, nil
}

func parseMode(value string, allowNone bool) (*task.Mode, error) {
	if allowNone && value == "none" {
		return nil, nil
	}
	mode := task.Mode(value)
	if mode != task.ModeTrivial && mode != task.ModeBounded && mode != task.ModeInvestigate {
		allowed := "trivial, bounded, or investigate"
		if allowNone {
			allowed += ", or none"
		}
		return nil, fmt.Errorf("--mode must be %s", allowed)
	}
	return &mode, nil
}

func parsePriority(value string, allowNone bool) (*task.Priority, error) {
	if allowNone && value == "none" {
		return nil, nil
	}
	priority := task.Priority(value)
	if priority != task.PriorityP0 && priority != task.PriorityP1 && priority != task.PriorityP2 && priority != task.PriorityP3 {
		allowed := "p0, p1, p2, or p3"
		if allowNone {
			allowed += ", or none"
		}
		return nil, fmt.Errorf("--priority must be %s", allowed)
	}
	return &priority, nil
}

func validBlocker(value string) bool {
	return strings.EqualFold(value, task.BlockerNeedsInfo) || strings.EqualFold(value, task.BlockerNeedsDiscussion)
}

type listResult struct {
	Repository string      `json:"repository"`
	Tasks      []task.Task `json:"tasks"`
}

type taskResult struct {
	Repository string     `json:"repository"`
	Task       *task.Task `json:"task"`
}

type mutationResult struct {
	Repository string        `json:"repository"`
	Changed    bool          `json:"changed"`
	DryRun     bool          `json:"dryRun"`
	Changes    []task.Change `json:"changes"`
	Task       *task.Task    `json:"task"`
}

func execute(ctx context.Context, out renderer, service *task.Service, repositoryName, command string, request commandRequest) int {
	switch command {
	case "list":
		tasks, err := service.List(ctx, repositoryName, request.state)
		if err != nil {
			return out.operational(err)
		}
		if out.json {
			return out.write(listResult{Repository: repositoryName, Tasks: tasks})
		}
		if len(tasks) == 0 {
			if _, err := fmt.Fprintln(out.stdout, "No tasks."); err != nil {
				return out.outputError(err)
			}
			return exitOK
		}
		for _, value := range tasks {
			if _, err := fmt.Fprintln(out.stdout, taskLine(value)); err != nil {
				return out.outputError(err)
			}
		}
		return exitOK
	case "show":
		value, err := service.Show(ctx, repositoryName, request.number, request.full)
		if err != nil {
			return out.operational(err)
		}
		if out.json {
			return out.write(taskResult{Repository: repositoryName, Task: &value})
		}
		if err := writeTaskDetail(out.stdout, value); err != nil {
			return out.outputError(err)
		}
		return exitOK
	case "next":
		value, err := service.Next(ctx, repositoryName)
		if err != nil {
			return out.operational(err)
		}
		if out.json {
			return out.write(taskResult{Repository: repositoryName, Task: value})
		}
		if value == nil {
			if _, err := fmt.Fprintln(out.stdout, "No ready task."); err != nil {
				return out.outputError(err)
			}
		} else {
			if _, err := fmt.Fprintln(out.stdout, taskLine(*value)); err != nil {
				return out.outputError(err)
			}
		}
		return exitOK
	default:
		result, err := service.Mutate(ctx, repositoryName, request.number, request.mutation, request.dryRun)
		if err != nil {
			return out.operational(err)
		}
		public := mutationResult{
			Repository: repositoryName, Changed: result.Changed, DryRun: result.DryRun,
			Changes: result.Changes, Task: result.Task,
		}
		if out.json {
			return out.write(public)
		}
		if err := writeMutation(out.stdout, command, request.number, public); err != nil {
			return out.outputError(err)
		}
		return exitOK
	}
}

type renderer struct {
	stdout io.Writer
	stderr io.Writer
	json   bool
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Hint    string        `json:"hint"`
	Changes []task.Change `json:"changes,omitempty"`
	Task    *task.Task    `json:"task,omitempty"`
}

func (r renderer) usage(message, hint string) int {
	return r.writeError(errorBody{Code: "invalid_usage", Message: message, Hint: hint}, exitUsage)
}

func (r renderer) operational(err error) int {
	body := errorBody{Code: "operation_failed", Message: "command failed", Hint: "Retry the command."}
	var mutationErr *task.MutationError
	var taskErr *task.Error
	var repositoryErr *repository.TaskResolveError
	switch {
	case errors.As(err, &mutationErr):
		body.Code, body.Message, body.Hint = mutationErr.Code, mutationErr.Message, mutationErr.Hint
		body.Changes, body.Task = mutationErr.Changes, mutationErr.Task
	case errors.As(err, &taskErr):
		body.Code, body.Message, body.Hint = taskErr.Code, taskErr.Message, taskErr.Hint
	case errors.As(err, &repositoryErr):
		body.Code, body.Message, body.Hint = repositoryErr.Code, repositoryErr.Message, repositoryErr.Hint
	case errors.Is(err, auth.ErrCredentialsNotFound):
		body.Code, body.Message = "auth_required", "GitHub credentials are required"
		body.Hint = "Set GITHUB_TOKEN or GH_TOKEN, or run `gh auth login`."
	}
	return r.writeError(body, exitOperational)
}

func (r renderer) outputError(_ error) int {
	return r.writeError(errorBody{
		Code: "output_error", Message: "write output failed",
		Hint: "Retry with a writable output stream.",
	}, exitOperational)
}

func (r renderer) writeError(body errorBody, code int) int {
	if !r.json {
		if err := writeTextError(r.stderr, body); err != nil {
			return exitOperational
		}
		return code
	}
	if err := writeJSON(r.stderr, errorEnvelope{Error: body}); err != nil {
		if _, fallbackErr := fmt.Fprintf(r.stderr, "encode JSON error: %v\n", err); fallbackErr != nil {
			return exitOperational
		}
	}
	return code
}

func (r renderer) write(value any) int {
	if err := writeJSON(r.stdout, value); err != nil {
		return r.outputError(err)
	}
	return exitOK
}

func writeTextError(writer io.Writer, body errorBody) error {
	if _, err := fmt.Fprintf(writer, "error: %s\n", body.Message); err != nil {
		return err
	}
	if body.Hint != "" {
		if _, err := fmt.Fprintf(writer, "hint: %s\n", body.Hint); err != nil {
			return err
		}
	}
	if len(body.Changes) > 0 {
		if _, err := fmt.Fprintln(writer, "confirmed changes:"); err != nil {
			return err
		}
		for _, change := range body.Changes {
			if change.Label == "" {
				if _, err := fmt.Fprintf(writer, "  %s\n", change.Action); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(writer, "  %s %s\n", change.Action, change.Label); err != nil {
				return err
			}
		}
	}
	if body.Task != nil {
		if _, err := fmt.Fprintf(writer, "current task: %s\n", taskLine(*body.Task)); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func taskLine(value task.Task) string {
	mode, priority := "-", "-"
	if value.Mode != nil {
		mode = string(*value.Mode)
	}
	if value.Priority != nil {
		priority = string(*value.Priority)
	}
	return fmt.Sprintf("#%d [%s] [%s] [%s] %s", value.Number, value.State, priority, mode, oneLine(value.Title))
}

func writeTaskDetail(writer io.Writer, value task.Task) error {
	if _, err := fmt.Fprintln(writer, taskLine(value)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "%s\nissue: %s; in progress: %t\n", value.URL, value.IssueState, value.InProgress); err != nil {
		return err
	}
	if len(value.Blockers) > 0 {
		if _, err := fmt.Fprintf(writer, "blockers: %s\n", strings.Join(value.Blockers, ", ")); err != nil {
			return err
		}
	}
	if len(value.ProjectLabels) > 0 {
		if _, err := fmt.Fprintf(writer, "labels: %s\n", strings.Join(value.ProjectLabels, ", ")); err != nil {
			return err
		}
	}
	if len(value.Reasons) > 0 {
		if _, err := fmt.Fprintf(writer, "reasons: %s\n", strings.Join(value.Reasons, ", ")); err != nil {
			return err
		}
	}
	if value.Body != nil {
		if _, err := fmt.Fprintln(writer, "\nBody:"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(writer, *value.Body); err != nil {
			return err
		}
		if value.BodyTruncated != nil && *value.BodyTruncated {
			if _, err := fmt.Fprintln(writer, "\n[truncated; rerun with --full]"); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeMutation(writer io.Writer, command string, number int, result mutationResult) error {
	if !result.Changed {
		if _, err := fmt.Fprintln(writer, "No changes."); err != nil {
			return err
		}
	} else if result.DryRun {
		if _, err := fmt.Fprintf(writer, "Would %s issue #%d:\n", command, number); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(writer, "%s issue #%d:\n", titleVerb(command), number); err != nil {
			return err
		}
	}
	for _, change := range result.Changes {
		if change.Label == "" {
			if _, err := fmt.Fprintf(writer, "  %s\n", change.Action); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(writer, "  %s %s\n", change.Action, change.Label); err != nil {
				return err
			}
		}
	}
	if result.Task != nil {
		if _, err := fmt.Fprintln(writer, taskLine(*result.Task)); err != nil {
			return err
		}
	}
	return nil
}

func oneLine(value string) string { return strings.Join(strings.Fields(value), " ") }

func titleVerb(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

type helpEntry struct {
	purpose string
	usage   string
}

var commandHelp = map[string]helpEntry{
	"list":     {"List enrolled Tasks.", "baton [--repo owner/name] [--json] list [--state open|closed|all]"},
	"show":     {"Show one enrolled Task.", "baton [--repo owner/name] [--json] show ISSUE [--full]"},
	"next":     {"Return one deterministic ready Task.", "baton [--repo owner/name] [--json] next"},
	"enroll":   {"Enroll an issue as a Task.", "baton [--repo owner/name] [--json] enroll ISSUE [--mode trivial|bounded|investigate] [--priority p0|p1|p2|p3] [--dry-run]"},
	"update":   {"Update fixed Task classification.", "baton [--repo owner/name] [--json] update ISSUE [--mode trivial|bounded|investigate|none] [--priority p0|p1|p2|p3|none] [--add-blocker needs-info|needs:discussion]... [--remove-blocker needs-info|needs:discussion]... [--dry-run]"},
	"unenroll": {"Reversibly unenroll a Task.", "baton [--repo owner/name] [--json] unenroll ISSUE [--dry-run]"},
	"start":    {"Add advisory activity to a Task.", "baton [--repo owner/name] [--json] start ISSUE [--dry-run]"},
	"stop":     {"Clear advisory activity from a Task.", "baton [--repo owner/name] [--json] stop ISSUE [--dry-run]"},
	"close":    {"Explicitly close a Task.", "baton [--repo owner/name] [--json] close ISSUE [--dry-run]"},
}

func printHelp(writer io.Writer) error {
	if _, err := fmt.Fprintln(writer, "Baton manages explicitly enrolled GitHub issue Tasks."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "\nUsage:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "  baton [--repo owner/name] [--json] COMMAND [ARGS]"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "  baton --version"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "\nCommands:"); err != nil {
		return err
	}
	for _, command := range commandOrder {
		if _, err := fmt.Fprintf(writer, "  %-8s %s\n", command, commandHelp[command].purpose); err != nil {
			return err
		}
	}
	return nil
}

func printCommandHelp(writer io.Writer, command string) error {
	entry := commandHelp[command]
	_, err := fmt.Fprintf(writer, "baton %s\n\n%s\n\nUsage:\n  %s\n", command, entry.purpose, entry.usage)
	return err
}
