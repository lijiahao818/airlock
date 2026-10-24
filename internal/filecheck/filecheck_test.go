package filecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lijiahao818/airlock/internal/cabin"
)

// writeFile 在 dir 下建立 rel 指定的文件，自动建立父目录。content 是测试用的假内容。
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// byPath 把检查结果按路径索引，便于断言。
func byPath(fs []cabin.Finding) map[string]cabin.Finding {
	m := make(map[string]cabin.Finding, len(fs))
	for _, f := range fs {
		m[f.Path] = f
	}
	return m
}

// wantReason 检查 AC-9：每条结果的 Reason 不为空。
func wantReason(t *testing.T, fs []cabin.Finding) {
	t.Helper()
	for _, f := range fs {
		if strings.TrimSpace(f.Reason) == "" {
			t.Errorf("%s 的 Reason 为空", f.Path)
		}
	}
}

func TestEntry(t *testing.T) {
	dir := t.TempDir()
	for rel, content := range map[string]string{
		".env":                         "FAKE_PASSWORD=fake",
		".env.local":                   "FAKE_KEY=fake",
		"config/.env.production":       "FAKE_SECRET=fake",
		"foo.env":                      "不是 .env 文件",
		".ssh/id_rsa":                  "fake private key",
		"certs/server.pem":             "-----BEGIN FAKE-----\n",
		"certs/server.key":             "fake",
		"notes/secret.txt":             "-----BEGIN PRIVATE KEY-----\nFAKE body\n",
		"AGENTS.md":                    "# fake instructions",
		"docs/CLAUDE.md":               "# fake",
		".claude/skills/demo/SKILL.md": "# fake skill",
		"src/main.go":                  "package fake",
	} {
		writeFile(t, dir, rel, content)
	}
	// AC-3：依赖目录，任何层级都算。目录下的文件不单独列出。
	for _, rel := range []string{"node_modules", ".venv", "pkg/sub/__pycache__"} {
		writeFile(t, dir, rel+"/index.js", "fake")
	}

	got, err := (Checker{}).Entry(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantReason(t, got)
	m := byPath(got)

	// AC-1：.env 和以 .env. 开头的文件，任何层级都算。
	for _, p := range []string{".env", ".env.local", "config/.env.production"} {
		if m[p].Kind != cabin.Excluded {
			t.Errorf("%s = %q，want excluded", p, m[p].Kind)
		}
	}
	if f, ok := m["foo.env"]; ok {
		t.Errorf("foo.env 不应产生结果：%+v", f)
	}

	// AC-2：私钥文件，按文件名和后缀判断。
	for _, p := range []string{".ssh/id_rsa", "certs/server.pem", "certs/server.key"} {
		if m[p].Kind != cabin.Excluded {
			t.Errorf("%s = %q，want excluded", p, m[p].Kind)
		}
	}
	// AC-2：文件开头的 PRIVATE KEY 标记。
	if m["notes/secret.txt"].Kind != cabin.Excluded {
		t.Errorf("notes/secret.txt = %q，want excluded（文件头有私钥标记）", m["notes/secret.txt"].Kind)
	}

	// AC-3：目录标为 Cached，Path 以 / 结尾。
	for _, p := range []string{"node_modules/", ".venv/", "pkg/sub/__pycache__/"} {
		if m[p].Kind != cabin.Cached {
			t.Errorf("%s = %q，want cached", p, m[p].Kind)
		}
	}
	for _, p := range []string{"node_modules/index.js", "pkg/sub/__pycache__/index.js"} {
		if _, ok := m[p]; ok {
			t.Errorf("依赖目录下的文件 %s 不应单独列出", p)
		}
	}

	// AC-4：agent 当指令读取的文件。
	for _, p := range []string{"AGENTS.md", "docs/CLAUDE.md", ".claude/skills/demo/SKILL.md"} {
		if m[p].Kind != cabin.Notice {
			t.Errorf("%s = %q，want notice", p, m[p].Kind)
		}
	}

	if _, ok := m["src/main.go"]; ok {
		t.Errorf("src/main.go 不应产生结果")
	}
}

// TestEntryMissingProject 检查错误路径：项目文件夹不存在时返回错误。
func TestEntryMissingProject(t *testing.T) {
	if _, err := (Checker{}).Entry(filepath.Join(t.TempDir(), "不存在")); err == nil {
		t.Fatal("项目文件夹不存在时，Entry 应返回错误")
	}
}

// TestEntrySkipsSymlink 检查拒绝路径（红线 6）：不跟随符号链接。
// 链接本身按名字（link.pem、node_modules）本可以命中规则，但不跟随就不能确认目标，所以不产生结果。
func TestEntrySkipsSymlink(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, outside, "id_rsa", "fake")
	project := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(project, "link.pem")); err != nil {
		t.Skipf("本机不能建立符号链接，跳过：%v", err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "node_modules")); err != nil {
		t.Skipf("本机不能建立符号链接，跳过：%v", err)
	}
	got, err := (Checker{}).Entry(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("符号链接不应产生结果：%+v", got)
	}
}
