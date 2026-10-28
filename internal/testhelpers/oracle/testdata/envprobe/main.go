// Command envprobe is the fixture CLI of the harness tests. It prints the
// names (never the values) of its environment variables, one per line.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "cwd":
			dir, _ := os.Getwd()
			fmt.Println(dir)
			return
		case "fail":
			os.Exit(3)
		}
	}
	var names []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Println(name)
	}
}
