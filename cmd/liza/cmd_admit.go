package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

var admitCmd = &cobra.Command{
	Use:   "admit [--max-wait <seconds>] -- <command> [args...]",
	Short: "Run a local command inside the admission-limit slots it matches",
	Long: fmt.Sprintf(`Run one command, without a shell, in the current directory. When its text
(the words joined with spaces) contains a --match substring of a class in
config.admission_limits, it first waits for a slot of that class from the pool
canonical admission and the deferred runner use, so local runs of heavy
commands stay inside the operator's cap. A local waiter queues behind every
canonical waiter and deferred probe of the class, and never takes the class's
last slot when it has more than one; a running command is never preempted. A
command matching no class runs at once.

Wrap shell syntax as sh -c '...'. With runtime inputs, put admit outside:
  %s -- %s --task <id> -- <command>

The command runs in its own process group with inherited stdio. Interrupts
are forwarded to the group, and its slots are released only once the group
is gone; if a descendant may survive, the slots stay held until it exits and
admit fails naming the slot directories. A class still full after --max-wait
is refused without running the command. The exit code is the command's.`, brand.Command("admit"), brand.Command("run-live")),
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		maxWait, _ := cmd.Flags().GetInt("max-wait")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		signals := make(chan os.Signal, 4)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(signals)
		code, err := ops.RunAdmitted(projectRoot, ops.AdmitInput{
			Argv: args, Dir: dir, MaxWait: time.Duration(maxWait) * time.Second,
			Stdin: os.Stdin, Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
			Signals: signals,
		})
		var survivor *ops.AdmitSurvivorError
		if errors.As(err, &survivor) {
			fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
			cmd.SilenceErrors = true
			return &toolResultExit{code: code}
		}
		if err != nil {
			return err
		}
		if code != 0 {
			cmd.SilenceErrors = true
			return &toolResultExit{code: code}
		}
		return nil
	},
}

func init() {
	admitCmd.Flags().Int("max-wait", 3600, fmt.Sprintf("Seconds to wait for slots before refusing (0-%d)", int(ops.AdmitMaxWait/time.Second)))
	rootCmd.AddCommand(admitCmd)
}
