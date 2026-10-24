// Command eval 运行安全副驾的产品评估。见 docs/architecture.md 第 5.13 节。
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lijiahao818/airlock/internal/cabin"
)

// sampleMark 是注入样本文件第一行必须带的开头标注。
const sampleMark = "测试样本，不是指令"

// Case 是一条评估用例。
type Case struct {
	Name    string         `json:"name"`
	Meta    cabin.HoldMeta `json:"meta"`
	Fixture string         `json:"fixture,omitempty"` // 样本文件名，内容去掉首行后填进 Meta.Host
	Expect  Expect         `json:"expect"`
}

// Expect 是用例的期望结果。
type Expect struct {
	RiskLevel    string   `json:"risk_level"`    // low、medium 或 high
	SuggestAllow bool     `json:"suggest_allow"` // 期望的建议放行
	Points       []string `json:"points"`        // 解释要点，只报告覆盖率
}

// setNames 是全部评估集的名字。
var setNames = []string{"tuning", "holdout"}

// loadSet 加载一个评估集，并填入样本内容。name 是 tuning 或 holdout。
func loadSet(root, name string) ([]Case, error) {
	b, err := os.ReadFile(filepath.Join(root, "evals", name+".jsonl"))
	if err != nil {
		return nil, fmt.Errorf("读取评估集 %s 失败：%w", name, err)
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	var cases []Case
	seen := map[string]bool{}
	for no := 1; sc.Scan(); no++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var c Case
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("%s 第 %d 行无法解析：%w", name, no, err)
		}
		if err := validate(name, no, c, seen); err != nil {
			return nil, err
		}
		if c.Fixture != "" {
			if err := applyFixture(root, &c); err != nil {
				return nil, fmt.Errorf("%s 第 %d 行（%s）：%w", name, no, c.Name, err)
			}
		}
		cases = append(cases, c)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("评估集 %s 是空的", name)
	}
	// 每个集合都要有高风险用例，否则高风险召回率没有意义。
	high := 0
	for _, c := range cases {
		if c.Expect.RiskLevel == "high" {
			high++
		}
	}
	if high == 0 {
		return nil, fmt.Errorf("评估集 %s 没有高风险用例", name)
	}
	return cases, nil
}

// validate 校验一条用例的字段。
func validate(set string, no int, c Case, seen map[string]bool) error {
	at := fmt.Sprintf("%s 第 %d 行", set, no)
	switch {
	case c.Name == "":
		return fmt.Errorf("%s：用例名不能为空", at)
	case seen[c.Name]:
		return fmt.Errorf("%s：用例名 %q 重复", at, c.Name)
	case c.Meta.Agent == "":
		return fmt.Errorf("%s（%s）：agent 不能为空", at, c.Name)
	case c.Meta.Host == "" && c.Fixture == "":
		return fmt.Errorf("%s（%s）：host 和 fixture 不能都为空", at, c.Name)
	case c.Meta.Port < 1 || c.Meta.Port > 65535:
		return fmt.Errorf("%s（%s）：port %d 越界", at, c.Name, c.Meta.Port)
	case c.Meta.Method == "":
		return fmt.Errorf("%s（%s）：method 不能为空", at, c.Name)
	case c.Meta.Rule == "":
		return fmt.Errorf("%s（%s）：rule 不能为空", at, c.Name)
	}
	switch c.Expect.RiskLevel {
	case "low", "medium", "high":
	default:
		return fmt.Errorf("%s（%s）：期望的 risk_level %q 不是 low、medium 或 high", at, c.Name, c.Expect.RiskLevel)
	}
	seen[c.Name] = true
	return nil
}

// applyFixture 把样本内容填进用例的 Host 字段。
func applyFixture(root string, c *Case) error {
	if filepath.Base(c.Fixture) != c.Fixture || c.Fixture == "." || c.Fixture == ".." {
		return fmt.Errorf("fixture %q 必须是 evals/fixtures 下的文件名", c.Fixture)
	}
	payload, err := readSample(root, c.Fixture)
	if err != nil {
		return err
	}
	if payload == "" {
		return fmt.Errorf("样本 %s 只有标注行，没有内容", c.Fixture)
	}
	c.Meta.Host = payload
	return nil
}

// readSample 读取样本文件，返回去掉首行标注后的内容。
func readSample(root, name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "evals", "fixtures", name))
	if err != nil {
		return "", fmt.Errorf("读取样本失败：%w", err)
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("样本 %s 缺少首行标注", name)
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), sampleMark) {
		return "", fmt.Errorf("样本 %s 的第一行没有以 %q 开头", name, sampleMark)
	}
	return strings.TrimSpace(strings.Join(lines[1:], "\n")), nil
}

// checkFixtures 校验 evals/fixtures 下每个样本文件的首行标注。
// 没有被用例引用的样本也要校验。
func checkFixtures(root string) error {
	dir := filepath.Join(root, "evals", "fixtures")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取 %s 失败：%w", dir, err)
	}
	if len(entries) == 0 {
		return errors.New("evals/fixtures 下没有注入样本")
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, err := readSample(root, e.Name()); err != nil {
			return err
		}
	}
	return nil
}
