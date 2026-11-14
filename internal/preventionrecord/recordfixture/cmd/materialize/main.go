// Command materialize builds the sample-record repository into a directory:
//
//	go run ./internal/preventionrecord/recordfixture/cmd/materialize <dir>
//
// <dir> must be absent or empty. The repository path is printed on stdout.
// No case expectations are printed: a run must read them from the repository.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/liza-mas/liza/internal/preventionrecord/recordfixture"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: materialize <dir>")
		return 2
	}
	repo, err := recordfixture.Build(args[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, repo.Dir)
	return 0
}
