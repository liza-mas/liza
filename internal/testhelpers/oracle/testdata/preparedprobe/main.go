// Command preparedprobe exercises caller-owned projects. Output is restricted
// to fixed names and booleans; environment values never reach diagnostics.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		refuse("operation")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		refuse("HOME")
	}
	dirs := []string{".", home, os.TempDir()}
	switch os.Args[1] {
	case "prepare-1":
		if !content("caller.txt", "caller fixture\n") {
			refuse("caller-fixture")
		}
		for _, dir := range dirs {
			write(filepath.Join(dir, "step-1"), "first\n")
		}
		write("state.txt", "first\n")
	case "prepare-2":
		requireFirst(dirs)
		write("step-2", "second\n")
		write("state.txt", "ready\n")
	case "tested":
		write("tested-invoked", "true\n")
		requireFirst(dirs)
		if !content("step-2", "second\n") || !content("state.txt", "ready\n") {
			refuse("second-preparation")
		}
		write("result", "final\n")
		write("state.txt", "complete\n")
	case "observe":
		fmt.Printf("sentinel=%t\n", content("step-1", "first\n"))
	case "environment":
		stdin, err := io.ReadAll(os.Stdin)
		if err != nil {
			refuse("stdin")
		}
		fmt.Printf("extra=%t\nstdin=%t\npath-empty=%t\nhost-absent=%t\n",
			os.Getenv("ORACLE_PREPARED_EXTRA") == "fixture", string(stdin) == "fixture input\n",
			os.Getenv("PATH") == "", os.Getenv("ORACLE_PREPARED_HOST_ONLY") == "")
		var names []string
		for _, pair := range os.Environ() {
			name, _, _ := strings.Cut(pair, "=")
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Println("names=" + strings.Join(names, ","))
	case "fail":
		fmt.Println("stdout=true")
		fmt.Fprintln(os.Stderr, "stderr=true")
		os.Exit(7)
	case "touch":
		marker := "executed"
		if len(os.Args) > 2 {
			marker = os.Args[2]
		}
		write(marker, "true\n")
	default:
		refuse("operation")
	}
}

func requireFirst(dirs []string) {
	for _, dir := range dirs {
		if !content(filepath.Join(dir, "step-1"), "first\n") {
			refuse("first-preparation")
		}
	}
}

func content(path, want string) bool {
	got, err := os.ReadFile(path)
	return err == nil && string(got) == want
}

func write(path, data string) {
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		refuse("write")
	}
}

func refuse(name string) {
	fmt.Fprintln(os.Stderr, name+"=false")
	os.Exit(9)
}
