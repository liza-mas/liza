// Command regen reproduces oracle goldens without changing the source worktree.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

type packageFlags []string

func (p *packageFlags) String() string { return strings.Join(*p, ",") }
func (p *packageFlags) Set(s string) error {
	clean := strings.TrimSuffix(s, "/...")
	if !strings.HasPrefix(clean, "./internal/oracle") || (clean != "./internal/oracle" && !strings.HasPrefix(clean, "./internal/oracle/")) ||
		"./"+path.Clean(clean) != clean || strings.ContainsAny(clean, "\\\x00\r\n\t ") {
		return fmt.Errorf("package filter must be a clean ./internal/oracle path with an optional recursive suffix")
	}
	*p = append(*p, s)
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, nil)
	cancel()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, regenerate regenerator) (code int) {
	flags := flag.NewFlagSet("regen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "repository (default: containing working directory)")
	mode := flags.String("mode", "check", "check or write-new")
	var packages packageFlags
	flags.Var(&packages, "pkg", "oracle package pattern (repeatable)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || (*mode != "check" && *mode != "write-new") {
		fmt.Fprintln(stderr, "error arguments: expected check or write-new, no positional arguments")
		return 2
	}
	if len(packages) == 0 {
		packages = packageFlags{"./internal/oracle/..."}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "error environment: HOME unavailable")
		return 2
	}
	root, err := os.MkdirTemp("", "oracle-regen-*")
	if err != nil {
		fmt.Fprintln(stderr, "error worktree: cannot create temporary root")
		return 2
	}
	e := &engine{root: root, commands: commands{home: home, path: os.Getenv("PATH"), tmp: root}, packages: packages, trees: map[string]map[string][]byte{}}
	defer func() {
		if err := e.cleanup(); err != nil {
			fmt.Fprintf(stderr, "error cleanup: %v\n", err)
			code = 2
		}
	}()
	if *repo == "" {
		b, err := e.commands.git(ctx, "", "rev-parse", "--show-toplevel")
		if err != nil {
			fmt.Fprintf(stderr, "error repository: %v\n", err)
			return 2
		}
		*repo = strings.TrimSpace(string(b))
	}
	e.repo, err = filepath.Abs(*repo)
	if err != nil {
		fmt.Fprintln(stderr, "error repository: invalid path")
		return 2
	}
	// Provenance authority is committed HEAD; an uncommitted record cannot
	// authorize a golden change. The working oracle tree is still overlaid.
	committed, err := e.oracleFile(ctx, "HEAD", ledgerPath)
	if err != nil {
		fmt.Fprintf(stderr, "error ledger HEAD: %v\n", err)
		return 2
	}
	l, err := parseLedger(committed)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	working, err := e.oracleFile(ctx, "working", ledgerPath)
	if err != nil || string(working) != string(committed) {
		fmt.Fprintln(stderr, "error ledger: working ledger differs from HEAD")
		return 2
	}
	if regenerate == nil {
		regenerate = e.commands.regenerate
	}
	e.regenerate = regenerate
	if *mode == "write-new" {
		if err := e.writeNew(ctx, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	code, err = e.compare(ctx, l, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return code
}
