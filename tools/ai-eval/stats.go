package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

// prInfo matches the output of:
// gh pr list --state merged --limit 500 --json number,mergedAt,labels,comments
type prInfo struct {
	Number   int       `json:"number"`
	MergedAt time.Time `json:"mergedAt"`
	Labels   []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Comments []struct {
		Body string `json:"body"`
	} `json:"comments"`
}

type dayStats struct {
	Day       string
	Merged    int
	Evaluated int
	FirstPass int
	Overrides int
}

// chartTheme sets a visible line color. The default theme draws a very light line on GitHub.
const chartTheme = `%%{init: {"themeVariables": {"xyChart": {"plotColorPalette": "#2563eb"}}}}%%` + "\n"

var markerRe = regexp.MustCompile(`<!-- ai-eval (\{.*?\}) -->`)

// beijing is fixed so the tool needs no time zone database.
var beijing = time.FixedZone("UTC+8", 8*60*60)

func runStats(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	prsPath := fs.String("prs", "", "gh pr list 输出的 JSON 文件")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var prs []prInfo
	if err := readJSON(*prsPath, &prs); err != nil {
		fmt.Fprintln(stderr, "读取 PR 数据失败：", err)
		return 2
	}
	fmt.Fprint(stdout, renderStats(collectStats(prs)))
	return 0
}

// firstVerdict returns the verdict of the first ai-eval report on a PR, or "" if none.
// It reads the last marker of a comment, because the tool always writes its marker last.
func firstVerdict(pr prInfo) string {
	for _, c := range pr.Comments {
		m := markerRe.FindAllStringSubmatch(c.Body, -1)
		if len(m) == 0 {
			continue
		}
		var v struct {
			Verdict string `json:"verdict"`
		}
		if json.Unmarshal([]byte(m[len(m)-1][1]), &v) == nil {
			return v.Verdict
		}
	}
	return ""
}

func collectStats(prs []prInfo) []dayStats {
	byDay := map[string]*dayStats{}
	for _, pr := range prs {
		day := pr.MergedAt.In(beijing).Format("01-02")
		s := byDay[day]
		if s == nil {
			s = &dayStats{Day: day}
			byDay[day] = s
		}
		s.Merged++
		if v := firstVerdict(pr); v != "" {
			s.Evaluated++
			if v == verdictPass {
				s.FirstPass++
			}
		}
		for _, l := range pr.Labels {
			if l.Name == "ai-eval-override" {
				s.Overrides++
			}
		}
	}
	days := make([]dayStats, 0, len(byDay))
	for _, s := range byDay {
		days = append(days, *s)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Day < days[j].Day })
	return days
}

func passRate(s dayStats) int {
	if s.Evaluated == 0 {
		return 0
	}
	return s.FirstPass * 100 / s.Evaluated
}

func renderStats(days []dayStats) string {
	var sb strings.Builder
	sb.WriteString("| 日期 | 合并的 PR 数 | AI 评估首次通过率 | 推翻次数 |\n|---|---|---|---|\n")
	var labels, merged, rateLabels, rates []string
	maxCount := 1
	for _, s := range days {
		rate := "无评估"
		if s.Evaluated > 0 {
			rate = fmt.Sprintf("%d%%（%d/%d）", passRate(s), s.FirstPass, s.Evaluated)
			rateLabels = append(rateLabels, fmt.Sprintf("%q", s.Day))
			rates = append(rates, fmt.Sprint(passRate(s)))
		}
		fmt.Fprintf(&sb, "| %s | %d | %s | %d |\n", s.Day, s.Merged, rate, s.Overrides)
		labels = append(labels, fmt.Sprintf("%q", s.Day))
		merged = append(merged, fmt.Sprint(s.Merged))
		maxCount = max(maxCount, s.Merged)
	}
	if len(days) == 0 {
		return sb.String()
	}
	fmt.Fprintf(&sb, "\n```mermaid\n%sxychart-beta\n    title \"每天合并的 PR 数\"\n    x-axis [%s]\n    y-axis \"PR 数\" 0 --> %d\n    line [%s]\n```\n",
		chartTheme, strings.Join(labels, ", "), maxCount, strings.Join(merged, ", "))
	if len(rates) > 0 {
		fmt.Fprintf(&sb, "\n```mermaid\n%sxychart-beta\n    title \"AI 评估首次通过率\"\n    x-axis [%s]\n    y-axis \"通过率 (%%)\" 0 --> 100\n    line [%s]\n```\n",
			chartTheme, strings.Join(rateLabels, ", "), strings.Join(rates, ", "))
	}
	return sb.String()
}
