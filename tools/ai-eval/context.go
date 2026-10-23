package main

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
)

// quotedRe matches text in backticks, such as `docs/architecture.md`.
var quotedRe = regexp.MustCompile("`([^`\\s]+)`")

// collectContext reads the repository files that the issues quote in backticks.
// The issue body is untrusted, so os.Root keeps every read inside root.
// Files that do not fit in limit are returned as omitted.
func collectContext(issues, root string, limit int) (string, []string) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return "", nil
	}
	defer r.Close()
	var sb strings.Builder
	var omitted []string
	seen := map[string]bool{}
	for _, m := range quotedRe.FindAllStringSubmatch(issues, -1) {
		rel := path.Clean(m[1])
		// CLAUDE.md is already in the rules input.
		if seen[rel] || rel == "CLAUDE.md" {
			continue
		}
		seen[rel] = true
		info, err := r.Stat(rel)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		b, err := r.ReadFile(rel)
		if err != nil {
			continue
		}
		if sb.Len()+len(b) > limit {
			omitted = append(omitted, rel)
			continue
		}
		fmt.Fprintf(&sb, "===== %s =====\n%s\n", rel, b)
	}
	return sb.String(), omitted
}
