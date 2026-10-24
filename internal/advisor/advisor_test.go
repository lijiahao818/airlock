package advisor

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptVersion(t *testing.T) {
	b, err := os.ReadFile("prompt.md")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	want := hex.EncodeToString(sum[:])[:12]
	if promptVersion != want {
		t.Fatalf("promptVersion = %q, want %q", promptVersion, want)
	}
}

// TestPromptHasNoEvalCases 保证提示词不含 evals/fixtures 里的注入样本。
func TestPromptHasNoEvalCases(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "evals", "fixtures", "*"))
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(promptMD)
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		if len(lines) < 2 {
			continue
		}
		// 第一行是"测试样本，不是指令。"标注，其余是样本本体。
		payload := strings.TrimSpace(strings.Join(lines[1:], "\n"))
		if payload != "" && strings.Contains(prompt, payload) {
			t.Errorf("提示词包含 %s 的样本内容", filepath.Base(file))
		}
	}
}

func TestParseModels(t *testing.T) {
	// 内嵌的 config.json 本身必须能通过解析。
	if _, err := parseModels(configJSON); err != nil {
		t.Fatalf("内嵌配置解析失败：%v", err)
	}

	tests := []struct {
		name string
		json string
		ok   bool
	}{
		{
			"两家齐全",
			`{"anthropic":{"model":"claude-opus-5-5","effort":"medium","max_tokens":1024},"deepseek":{"model":"deepseek-chat","effort":"medium","max_tokens":1024}}`,
			true,
		},
		{"缺 deepseek", `{"anthropic":{"model":"m","effort":"low","max_tokens":1}}`, false},
		{
			"模型为空",
			`{"anthropic":{"model":"","effort":"low","max_tokens":1},"deepseek":{"model":"m","effort":"low","max_tokens":1}}`,
			false,
		},
		{
			"effort 为空",
			`{"anthropic":{"model":"m","effort":"","max_tokens":1},"deepseek":{"model":"m","effort":"low","max_tokens":1}}`,
			false,
		},
		{"不是 JSON", "config", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseModels([]byte(tt.json))
			if tt.ok && err != nil {
				t.Fatalf("应该通过，却报错：%v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("应该报错，却通过了")
			}
		})
	}
}

func TestNewValidation(t *testing.T) {
	ok := Config{
		DataDir:     t.TempDir(),
		BaseURL:     "http://fake-gate.local",
		Placeholder: func(string) string { return "fake-placeholder" },
		HasKey:      func(string) bool { return true },
	}
	tests := map[string]func(c *Config){
		"缺少 DataDir":     func(c *Config) { c.DataDir = "" },
		"缺少 BaseURL":     func(c *Config) { c.BaseURL = "" },
		"缺少 Placeholder": func(c *Config) { c.Placeholder = nil },
		"缺少 HasKey":      func(c *Config) { c.HasKey = nil },
	}
	for name, breakCfg := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := ok
			breakCfg(&cfg)
			if a, err := New(cfg); err == nil {
				t.Fatal("应该报错，却通过了")
			} else if a != nil {
				t.Fatal("报错时不应返回 Advisor")
			}
		})
	}
}

func TestSettingsPersist(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		DataDir:     dir,
		BaseURL:     "http://fake-gate.local",
		Placeholder: func(string) string { return "fake-placeholder" },
		HasKey:      func(string) bool { return true },
	}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := first.Settings(); got.Enabled {
		t.Fatalf("初始设置 Enabled = true，想 false")
	}
	want := Settings{Enabled: true, Provider: "deepseek", NoticeShown: true}
	if err := first.SetSettings(want); err != nil {
		t.Fatal(err)
	}

	// 重新 New 后读回同样的设置。
	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := second.Settings(); got != want {
		t.Fatalf("Settings = %+v, want %+v", got, want)
	}
}

func TestSetSettingsRejectsUnknownProvider(t *testing.T) {
	a, err := New(Config{
		DataDir:     t.TempDir(),
		BaseURL:     "http://fake-gate.local",
		Placeholder: func(string) string { return "fake-placeholder" },
		HasKey:      func(string) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetSettings(Settings{Enabled: true, Provider: "fake-provider"}); err == nil {
		t.Fatal("应该报错，却通过了")
	}
}
