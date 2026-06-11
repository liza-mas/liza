package toolchain

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
)

type Command struct {
	Name string            `json:"name"`
	Args []string          `json:"args,omitempty"`
	Dir  string            `json:"dir,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
}

type CommandOutput struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
}

type Runner interface {
	LookPath(name string) (string, error)
	Run(command Command) (CommandOutput, error)
}

type RealRunner struct{}

func (RealRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (RealRunner) Run(command Command) (CommandOutput, error) {
	cmd := exec.Command(command.Name, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = os.Environ()
	for key, value := range command.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		exitCode = -1
	}
	return CommandOutput{
		Stdout:   strings.TrimSpace(stdout.String()),
		Stderr:   strings.TrimSpace(stderr.String()),
		ExitCode: exitCode,
	}, err
}

func runnerOrDefault(r Runner) Runner {
	if r == nil {
		return RealRunner{}
	}
	return r
}
