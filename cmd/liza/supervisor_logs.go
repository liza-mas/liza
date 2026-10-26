package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/secretmask"
	"github.com/spf13/cobra"
)

// Permissions of the supervisor log files and of the bootstrap status file.
const (
	supervisorLogFileMode       = 0o644
	supervisorBootstrapFileMode = 0o600
)

type supervisorLogs struct {
	stderr        io.Writer
	persist       bool
	stdoutFile    *os.File
	stderrFile    *os.File
	stdoutWriter  *secretmask.StreamingWriter
	stderrWriter  *secretmask.StreamingWriter
	restoreLogger func()
}

func openSupervisorLogs(cmd *cobra.Command) (*supervisorLogs, error) {
	stdoutPath, _ := cmd.Flags().GetString(agent.SupervisorStdoutLogFlag)
	stderrPath, _ := cmd.Flags().GetString(agent.SupervisorStderrLogFlag)
	if stdoutPath == "" && stderrPath == "" {
		return &supervisorLogs{stderr: cmd.ErrOrStderr()}, nil
	}
	if stdoutPath == "" || stderrPath == "" {
		return nil, fmt.Errorf("--%s and --%s must be supplied together",
			agent.SupervisorStdoutLogFlag, agent.SupervisorStderrLogFlag)
	}

	stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, supervisorLogFileMode)
	if err != nil {
		return nil, fmt.Errorf("create supervisor stdout log: %w", err)
	}
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, supervisorLogFileMode)
	if err != nil {
		closeErr := stdoutFile.Close()
		removeErr := os.Remove(stdoutPath)
		return nil, errors.Join(
			fmt.Errorf("create supervisor stderr log: %w", err),
			closeErr,
			removeErr,
		)
	}

	masker := agent.NewSecretMasker()
	stdoutWriter := masker.NewStreamingWriter(stdoutFile)
	stderrWriter := masker.NewStreamingWriter(stderrFile)
	return &supervisorLogs{
		stderr:        stderrWriter,
		persist:       true,
		stdoutFile:    stdoutFile,
		stderrFile:    stderrFile,
		stdoutWriter:  stdoutWriter,
		stderrWriter:  stderrWriter,
		restoreLogger: agent.UseLoggerOutput(stdoutWriter),
	}, nil
}

func writeSupervisorBootstrapReady(cmd *cobra.Command) error {
	return writeSupervisorBootstrapStatus(cmd, agent.SupervisorBootstrapReadyStatus)
}

func writeSupervisorBootstrapError(cmd *cobra.Command, cause error) error {
	masked := agent.NewSecretMasker().MaskText(cause.Error())
	return writeSupervisorBootstrapStatus(cmd, agent.SupervisorBootstrapErrorPrefix+masked+"\n")
}

func writeSupervisorBootstrapStatus(cmd *cobra.Command, status string) error {
	path, _ := cmd.Flags().GetString(agent.SupervisorReadyFileFlag)
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, supervisorBootstrapFileMode)
	if err != nil {
		return fmt.Errorf("write supervisor bootstrap status: %w", err)
	}
	_, writeErr := io.WriteString(file, status)
	return errors.Join(writeErr, file.Close())
}

func (l *supervisorLogs) Close() error {
	if l == nil {
		return nil
	}
	if l.restoreLogger != nil {
		l.restoreLogger()
		l.restoreLogger = nil
	}
	return errors.Join(
		flushWriter(l.stdoutWriter),
		flushWriter(l.stderrWriter),
		closeFile(l.stdoutFile),
		closeFile(l.stderrFile),
	)
}

func finishSupervisorLogs(logs *supervisorLogs, runErr error) error {
	if runErr != nil && logs.persist {
		if _, err := fmt.Fprintf(logs.stderr, "Error: %v\n", runErr); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("write supervisor error log: %w", err))
		}
	}
	return errors.Join(runErr, logs.Close())
}

func finishSupervisorRun(logs *supervisorLogs, runErr error, recovered any) error {
	if recovered != nil {
		if !logs.persist {
			panic(recovered)
		}
		panicErr := fmt.Errorf("supervisor panic: %v", recovered)
		if _, err := fmt.Fprintf(logs.stderr, "%v\n%s", panicErr, debug.Stack()); err != nil {
			panicErr = errors.Join(panicErr, fmt.Errorf("write supervisor panic log: %w", err))
		}
		runErr = errors.Join(runErr, panicErr)
	}
	return finishSupervisorLogs(logs, runErr)
}

func flushWriter(writer *secretmask.StreamingWriter) error {
	if writer == nil {
		return nil
	}
	return writer.Flush()
}

func closeFile(file *os.File) error {
	if file == nil {
		return nil
	}
	return file.Close()
}
