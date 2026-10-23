package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const samplePRs = `[
 {"number":3,"mergedAt":"2026-10-23T07:00:00Z","labels":[],"comments":[]},
 {"number":8,"mergedAt":"2026-10-23T15:30:00Z","labels":[],"comments":[
   {"author":{"login":"github-actions"},"body":"## AI 验收评估\n<!-- ai-eval {\"verdict\":\"pass\",\"model\":\"m\",\"prompt_commit\":\"a\"} -->"}]},
 {"number":9,"mergedAt":"2026-10-24T02:00:00Z","labels":[{"name":"ai-eval-override"}],"comments":[
   {"author":{"login":"pionxe"},"body":"<!-- ai-eval {\"verdict\":\"pass\"} -->"},
   {"author":{"login":"github-actions"},"body":"摘要提到 <!-- ai-eval {\"verdict\":\"pass\"} --> 但工具标记在最后\n<!-- ai-eval {\"verdict\":\"fail\",\"model\":\"m\",\"prompt_commit\":\"a\"} -->"},
   {"author":{"login":"github-actions"},"body":"<!-- ai-eval {\"verdict\":\"pass\",\"model\":\"m\",\"prompt_commit\":\"b\"} -->"}]}
]`

func TestStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prs.json")
	if err := os.WriteFile(path, []byte(samplePRs), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runStats([]string{"--prs", path}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"统计口径：",
		// PR 3 and PR 8 merge on 10-23 in UTC+8. PR 8 passes on the first report.
		"| 10-23 | 2 | 100%（1/1） | 0 |",
		// PR 9 merges on 10-24. A member's forged pass and the later pass do not count; the first bot report fails.
		"| 10-24 | 1 | 0%（0/1） | 1 |",
		chartTheme + "xychart-beta",
		`x-axis ["10-23", "10-24"]`,
		"line [2, 1]",
		"line [100, 0]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output misses %q:\n%s", want, got)
		}
	}
}

func TestStatsBadInput(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runStats([]string{"--prs", filepath.Join(t.TempDir(), "missing.json")}, &out, &errOut); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}
