package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/brandrender"
)

func main() {
	repoRoot := flag.String("repo-root", ".", "repository root")
	flag.Parse()

	values := brand.ValuesFromEnv(os.Getenv)
	if err := brandrender.SyncEmbedded(brandrender.SyncOptions{RepoRoot: *repoRoot, Values: values}); err != nil {
		fmt.Fprintf(os.Stderr, "sync embedded: %v\n", err)
		os.Exit(1)
	}
}
