package util

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type ExecResult struct {
	Stdout string
	Stderr string
	Err    error
}

func ExecTimeout(timeout time.Duration, name string, args ...string) ExecResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		err = fmt.Errorf("command %q timed out after %v: %w", name, timeout, ctx.Err())
	}

	return ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    err,
	}
}

func Exec(name string, args ...string) ExecResult {
	return ExecTimeout(10*time.Second, name, args...)
}
