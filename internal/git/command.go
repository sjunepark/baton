package git

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

type CommandError struct {
	Args     []string
	ExitCode int
	Stderr   string
	Cause    error
}

func (e *CommandError) Error() string {
	return "git command failed"
}

func (e *CommandError) Unwrap() error { return e.Cause }

func Available() error {
	_, err := exec.LookPath("git")
	return err
}

// Output is the single process boundary for git commands executed by Baton.
func Output(args ...string) (string, error) {
	return OutputContext(context.Background(), args...)
}

func OutputAt(root string, args ...string) (string, error) {
	return OutputAtContext(context.Background(), root, args...)
}

func OutputContext(ctx context.Context, args ...string) (string, error) {
	return OutputAtContext(ctx, "", args...)
}

func OutputAtContext(ctx context.Context, root string, args ...string) (string, error) {
	commandArgs := append([]string(nil), args...)
	if root != "" {
		commandArgs = append([]string{"-C", root}, commandArgs...)
	}
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return stdout.String(), &CommandError{
			Args: commandArgs, ExitCode: exitCode,
			Stderr: strings.TrimSpace(stderr.String()), Cause: err,
		}
	}
	return stdout.String(), nil
}

func IsExitCode(err error, code int) bool {
	var commandError *CommandError
	return errors.As(err, &commandError) && commandError.ExitCode == code
}

func StderrContains(err error, value string) bool {
	var commandError *CommandError
	return errors.As(err, &commandError) && strings.Contains(commandError.Stderr, value)
}
