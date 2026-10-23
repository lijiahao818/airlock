// Command ai-eval runs the AI acceptance review on a pull request and
// summarizes AI efficiency metrics. See CLAUDE.md section 12.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "review":
			return runReview(context.Background(), args[1:], stdout, stderr)
		case "stats":
			return runStats(args[1:], stdout, stderr)
		}
	}
	fmt.Fprintln(stderr, "用法：ai-eval review|stats [参数]")
	return 2
}
