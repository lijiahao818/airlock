package filecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lijiahao818/airlock/internal/cabin"
	"github.com/lijiahao818/airlock/internal/staging"
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
		// 大小写变体放在独立子目录：Windows 的文件系统不区分大小写，同一目录放不下两个变体。
		"case/.ENV":       "FAKE=fake",
		"case/ID_RSA":     "fake",
		"case/Server.PEM": "fake",
		"case/Agents.md":  "# fake",
	} {
		writeFile(t, dir, rel, content)
	}
	// AC-3：依赖目录，任何层级都算。目录下的文件不单独列出。
	for _, rel := range []string{"node_modules", ".venv", "pkg/sub/__pycache__", "case/Target"} {
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
	for _, p := range []string{"AGENTS.md", "docs/CLAUDE.md", ".claude/skills/demo/SKILL.md", "case/Agents.md"} {
		if m[p].Kind != cabin.Notice {
			t.Errorf("%s = %q，want notice", p, m[p].Kind)
		}
	}

	// 大小写变体：宿主机不区分大小写，匹配必须不区分大小写。
	for _, p := range []string{"case/.ENV", "case/ID_RSA", "case/Server.PEM"} {
		if m[p].Kind != cabin.Excluded {
			t.Errorf("%s = %q，want excluded", p, m[p].Kind)
		}
	}
	if m["case/Target/"].Kind != cabin.Cached {
		t.Errorf("case/Target/ = %q，want cached", m["case/Target/"].Kind)
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

func TestExit(t *testing.T) {
	files := t.TempDir()
	// AC-7：按文件头判断的样本。二进制内容是测试用的假数据。
	for rel, head := range map[string]string{
		"bin/tool":     "\x7fELF\x02fake",
		"dist/app.exe": "MZfake",
		"run/start.sh": "#!/bin/sh\nfake\n",
		"run/hash.txt": "#",
		"src/main.go":  "package fake",
	} {
		writeFile(t, files, rel, head)
	}

	got := (Checker{}).Exit(files, []staging.Change{
		// AC-5：依赖目录下的改动，任何层级、任何类型都算。
		{Path: "node_modules/left-pad/index.js", Kind: staging.Added},
		{Path: "pkg/sub/__pycache__/main.pyc", Kind: staging.Modified},
		// AC-6：Git hooks，包括 .git/config（改 core.hooksPath 同样能执行代码）。
		{Path: ".git/hooks/pre-commit", Kind: staging.Added},
		{Path: "sub/.git/hooks/pre-push", Kind: staging.Added},
		{Path: ".git/config", Kind: staging.Modified},
		// AC-6：CI 配置，包括 composite action 和 husky 钩子目录。
		{Path: ".github/workflows/ci.yml", Kind: staging.Modified},
		{Path: ".circleci/config.yml", Kind: staging.Modified},
		{Path: ".github/actions/build/action.yml", Kind: staging.Added},
		{Path: ".husky/pre-commit", Kind: staging.Added},
		{Path: "Jenkinsfile", Kind: staging.Added},
		{Path: ".gitlab-ci.yml", Kind: staging.Modified},
		{Path: ".travis.yml", Kind: staging.Deleted},
		// AC-6：依赖安装脚本。
		{Path: "scripts/install.sh", Kind: staging.Added},
		{Path: "setup.py", Kind: staging.Added},
		// 大小写变体：沙箱是 Linux，写回的宿主机是 Windows，不区分大小写。
		{Path: ".Git/Hooks/pre-commit", Kind: staging.Added},
		{Path: ".GitHub/workflows/ci.yml", Kind: staging.Modified},
		{Path: "Node_Modules/left-pad/index.js", Kind: staging.Added},
		{Path: "INSTALL.SH", Kind: staging.Added},
		// AC-7：新增的可执行文件。
		{Path: "bin/tool", Kind: staging.Added},
		{Path: "dist/app.exe", Kind: staging.Added},
		{Path: "run/start.sh", Kind: staging.Added},
		// 不产生结果的改动。
		{Path: "run/hash.txt", Kind: staging.Added},   // 单字节 #，不误报
		{Path: "src/main.go", Kind: staging.Modified}, // 文本改动
		{Path: "README.md", Kind: staging.Modified},   // 普通改动
	})
	wantReason(t, got)
	m := byPath(got)

	for _, p := range []string{"node_modules/left-pad/index.js", "pkg/sub/__pycache__/main.pyc"} {
		if m[p].Kind != cabin.Cached {
			t.Errorf("%s = %q，want cached", p, m[p].Kind)
		}
	}
	for _, p := range []string{
		".git/hooks/pre-commit", "sub/.git/hooks/pre-push", ".git/config",
		".github/workflows/ci.yml", ".circleci/config.yml",
		".github/actions/build/action.yml", ".husky/pre-commit",
		"Jenkinsfile", ".gitlab-ci.yml", ".travis.yml",
		"scripts/install.sh", "setup.py",
		".Git/Hooks/pre-commit", ".GitHub/workflows/ci.yml", "INSTALL.SH",
	} {
		if m[p].Kind != cabin.HighRisk {
			t.Errorf("%s = %q，want high-risk", p, m[p].Kind)
		}
	}
	if m[".git/config"].Reason != reasonGit {
		t.Errorf(".git/config 的 Reason = %q，want %q", m[".git/config"].Reason, reasonGit)
	}
	if m[".husky/pre-commit"].Reason != reasonGitHook {
		t.Errorf(".husky/pre-commit 的 Reason = %q，want %q", m[".husky/pre-commit"].Reason, reasonGitHook)
	}
	if m["Node_Modules/left-pad/index.js"].Kind != cabin.Cached {
		t.Errorf("Node_Modules/left-pad/index.js = %q，want cached", m["Node_Modules/left-pad/index.js"].Kind)
	}
	if m["bin/tool"].Reason != reasonExecutable {
		t.Errorf("bin/tool 的 Reason = %q，want %q", m["bin/tool"].Reason, reasonExecutable)
	}
	for _, p := range []string{"run/hash.txt", "src/main.go", "README.md"} {
		if _, ok := m[p]; ok {
			t.Errorf("%s 不应产生结果：%+v", p, m[p])
		}
	}
}

// TestExitModifiedExecutable 检查 AC-7 只判断新增文件：改动的可执行文件按名字规则处理，不读文件头。
func TestExitModifiedExecutable(t *testing.T) {
	files := t.TempDir()
	writeFile(t, files, "bin/tool", "\x7fELFfake")
	got := (Checker{}).Exit(files, []staging.Change{{Path: "bin/tool", Kind: staging.Modified}})
	if len(got) != 0 {
		t.Errorf("修改的可执行文件不应按文件头产生结果：%+v", got)
	}
}

// TestExitBadPath 检查拒绝路径（红线 6、AC-8）：越权路径不读取文件，直接标为 HighRisk。
// 诱饵文件放在越权路径拼接后能到达的位置。如果 Exit 读了文件，Reason 会变成可执行文件的解释。
func TestExitBadPath(t *testing.T) {
	root := t.TempDir()
	files := filepath.Join(root, "files")
	writeFile(t, files, "etc/evil.sh", "\x7fELFfake")
	writeFile(t, root, "evil.sh", "\x7fELFfake")

	for _, p := range []string{"../evil.sh", "a/../../evil.sh", "/etc/evil.sh", "C:/evil.sh", `evil\evil.sh`, ""} {
		got := (Checker{}).Exit(files, []staging.Change{{Path: p, Kind: staging.Added}})
		if len(got) != 1 || got[0].Kind != cabin.HighRisk || got[0].Reason != reasonBadPath {
			t.Errorf("Exit(%q) = %+v，want 路径异常的 HighRisk，并且不读取文件", p, got)
		}
	}
}

// TestExitSpecial 检查拒绝路径（红线 6、AC-8）：special 标记的文件和符号链接不按文件头判定。
func TestExitSpecial(t *testing.T) {
	files := t.TempDir()
	writeFile(t, files, "run/evil.sh", "\x7fELFfake")
	if got := (Checker{}).Exit(files, []staging.Change{{Path: "run/evil.sh", Kind: staging.Added, Special: true}}); len(got) != 0 {
		t.Errorf("special 文件不应产生结果：%+v", got)
	}

	outside := t.TempDir()
	writeFile(t, outside, "tool", "\x7fELFfake")
	if err := os.Symlink(filepath.Join(outside, "tool"), filepath.Join(files, "link.sh")); err != nil {
		t.Skipf("本机不能建立符号链接，跳过：%v", err)
	}
	if got := (Checker{}).Exit(files, []staging.Change{{Path: "link.sh", Kind: staging.Added}}); len(got) != 0 {
		t.Errorf("符号链接不应产生结果：%+v", got)
	}
}
