package runner

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
)

type Request struct {
	Kind    string
	Command string
	Dir     string
	Env     []string
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

func Run(ctx context.Context, req Request) (Result, error) {
	args := shellCommand(req.Kind, req.Command)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode(err),
	}
	return result, err
}

func shellCommand(kind, command string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/C", command}
	}
	return []string{"/bin/sh", "-c", command}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 1
}
