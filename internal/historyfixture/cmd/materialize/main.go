// Command materialize builds the fix-history fixture into a directory:
//
//	go run ./internal/historyfixture/cmd/materialize <dir>
//
// <dir> must be absent or empty. The upstream and downstream repository paths
// are printed on stdout, one per line, in that order. No case data is printed:
// a run must read everything from the repositories.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/liza-mas/liza/internal/historyfixture"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: materialize <dir>")
		return 2
	}
	h, err := historyfixture.Build(args[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, h.Upstream.Dir)
	fmt.Fprintln(stdout, h.Downstream.Dir)
	return 0
}
