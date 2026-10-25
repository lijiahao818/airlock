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
		// AC-1（Issue #74）：常见凭据文件。内容全用 fake- 前缀的假令牌。
		".npmrc":                 "//registry.npmjs.org/:_authToken=fake-npm-token",
		".yarnrc.yml":            "npmRegistries:\n  npmAuthToken: fake-yarn-token",
		".pypirc":                "[pypi]\npassword = fake-pypi-token",
		".netrc":                 "machine github.com\npassword fake-netrc-token",
		".git-credentials":       "https://fake-user:fake-git-token@github.com",
		".aws/credentials":       "[default]\naws_access_key_id = fake-aws-id",
		".docker/config.json":    "{\"auths\": {\"fake-registry\": {\"auth\": \"fake-docker-auth\"}}}",
		"infra/.aws/credentials": "[default]\naws_access_key_id = fake-aws-id",
		"certs/client.p12":       "fake-p12",
		"certs/client.pfx":       "fake-pfx",
		"sign/app.jks":           "fake-jks",
		"sign/app.keystore":      "fake-keystore",
		"deploy/prod.tfvars":     "db_password = \"fake-tf-token\"",
		// 反例：名字相近但不在名单中。
		"notes/.npmrc.txt":           "不是 npm 读取的 .npmrc",
		"vendor/foo.aws/credentials": "目录名不是 .aws，按路径段匹配不排除",
		// 大小写变体放在独立子目录：Windows 的文件系统不区分大小写，同一目录放不下两个变体。
		"case/.ENV":             "FAKE=fake",
		"case/ID_RSA":           "fake",
		"case/Server.PEM":       "fake",
		"case/Agents.md":        "# fake",
		"case/.NPMRC":           "fake-npm-token",
		"case/Prod.TFVARS":      "fake-tf-token",
		"case/.Aws/credentials": "fake-aws-id",
		// pkcs12 导出格式：标记出现在 100 字节以后。内容是测试用的假私钥。
		"case/export.txt": "Bag Attributes\n    localKeyID: fake fake fake fake fake\n    friendlyName: fake\nsubject=CN=fake\n-----BEGIN PRIVATE KEY-----\n",
	} {
		writeFile(t, dir, rel, content)
	}
	// AC-3：依赖目录，任何层级都算。目录下的文件不单独列出。
	for _, rel := range []string{"node_modules", ".venv", "pkg/sub/__pycache__", "case/Target"} {
		writeFile(t, dir, rel+"/index.js", "fake")
	}
	// .git 里的压缩对象不做内容嗅探，整个目录跳过。
	for _, rel := range []string{".git/hooks/pre-commit", ".git/objects/ab/cdef1234"} {
		writeFile(t, dir, rel, "fake")
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
	// AC-2：pkcs12 导出的文件以 Bag Attributes 开头，标记在 64 字节以后。
	if m["case/export.txt"].Kind != cabin.Excluded || m["case/export.txt"].Reason != reasonKeyContent {
		t.Errorf("case/export.txt = %+v，want excluded（嗅探范围要覆盖 Bag Attributes）", m["case/export.txt"])
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

	// AC-1（Issue #74）：凭据文件名单，按文件名、按路径、按后缀，任何层级都算。
	for _, p := range []string{
		".npmrc", ".yarnrc.yml", ".pypirc", ".netrc", ".git-credentials",
		".aws/credentials", ".docker/config.json", "infra/.aws/credentials",
		"certs/client.p12", "certs/client.pfx", "sign/app.jks", "sign/app.keystore", "deploy/prod.tfvars",
	} {
		if m[p].Kind != cabin.Excluded {
			t.Errorf("%s = %q，want excluded", p, m[p].Kind)
		}
	}
	if m[".npmrc"].Reason != reasonCredName || m[".aws/credentials"].Reason != reasonCredExt {
		t.Errorf("凭据文件的理由不对：%q / %q", m[".npmrc"].Reason, m[".aws/credentials"].Reason)
	}
	for _, p := range []string{"notes/.npmrc.txt", "vendor/foo.aws/credentials"} {
		if _, ok := m[p]; ok {
			t.Errorf("%s 不在名单中，不应产生结果", p)
		}
	}

	// AC-3（Issue #74）：大小写变体同样排除。
	for _, p := range []string{"case/.NPMRC", "case/Prod.TFVARS", "case/.Aws/credentials"} {
		if m[p].Kind != cabin.Excluded {
			t.Errorf("%s = %q，want excluded", p, m[p].Kind)
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
	// .git 跳过后，其中的文件不产生任何结果。
	for _, p := range []string{".git/hooks/pre-commit", ".git/objects/ab/cdef1234"} {
		if _, ok := m[p]; ok {
			t.Errorf("%s 不应产生结果（.git 跳过）：%+v", p, m[p])
		}
	}
}

// TestEntryMissingProject 检查错误路径：项目文件夹不存在时返回错误。
func TestEntryMissingProject(t *testing.T) {
	if _, err := (Checker{}).Entry(filepath.Join(t.TempDir(), "不存在")); err == nil {
		t.Fatal("项目文件夹不存在时，Entry 应返回错误")
	}
}

// TestEntryExcludesSymlink 检查拒绝路径（红线 6、AC-8）：不跟随符号链接，链接本身按默认拒绝排除。
// 链接按名字（link.pem、node_modules）本可以命中别的规则，但不跟随就不能确认目标，一律标 Excluded。
func TestEntryExcludesSymlink(t *testing.T) {
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
	if len(got) != 2 {
		t.Fatalf("符号链接应各产生一条 Excluded：%+v", got)
	}
	for _, f := range got {
		if f.Kind != cabin.Excluded || f.Reason != reasonSymlink {
			t.Errorf("%s = %q（%q），want excluded（符号链接）", f.Path, f.Kind, f.Reason)
		}
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

	got := (Checker{}).Exit(t.TempDir(), files, []staging.Change{
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
		// AC-1（Issue #75）：能自动执行命令的配置。
		{Path: ".envrc", Kind: staging.Added},
		{Path: "sub/.envrc", Kind: staging.Modified},
		{Path: ".vscode/tasks.json", Kind: staging.Added},
		{Path: ".devcontainer/devcontainer.json", Kind: staging.Modified},
		{Path: ".devcontainer/Dockerfile", Kind: staging.Added},
		{Path: "AGENTS.md", Kind: staging.Modified},
		{Path: "CLAUDE.md", Kind: staging.Modified},
		{Path: ".claude/skills/x/SKILL.md", Kind: staging.Added},
		// 大小写变体：沙箱是 Linux，写回的宿主机是 Windows，不区分大小写。
		{Path: ".Git/Hooks/pre-commit", Kind: staging.Added},
		{Path: ".GitHub/workflows/ci.yml", Kind: staging.Modified},
		{Path: "Node_Modules/left-pad/index.js", Kind: staging.Added},
		{Path: "INSTALL.SH", Kind: staging.Added},
		{Path: ".VSCODE/tasks.json", Kind: staging.Added},
		{Path: ".Envrc", Kind: staging.Added},
		{Path: ".DevContainer/devcontainer.json", Kind: staging.Added},
		{Path: "Agents.md", Kind: staging.Modified},
		// AC-7：新增的可执行文件。
		{Path: "bin/tool", Kind: staging.Added},
		{Path: "dist/app.exe", Kind: staging.Added},
		{Path: "run/start.sh", Kind: staging.Added},
		// 不产生结果的改动。
		{Path: "run/hash.txt", Kind: staging.Added},             // 单字节 #，不误报
		{Path: "src/main.go", Kind: staging.Modified},           // 文本改动
		{Path: "README.md", Kind: staging.Modified},             // 普通改动
		{Path: ".vscode/settings.json", Kind: staging.Modified}, // 不是任务文件
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
		".envrc", "sub/.envrc", ".vscode/tasks.json",
		".devcontainer/devcontainer.json", ".devcontainer/Dockerfile",
		"AGENTS.md", "CLAUDE.md", ".claude/skills/x/SKILL.md",
		".VSCODE/tasks.json", ".Envrc", ".DevContainer/devcontainer.json", "Agents.md",
	} {
		if m[p].Kind != cabin.HighRisk {
			t.Errorf("%s = %q，want high-risk", p, m[p].Kind)
		}
	}
	if m[".envrc"].Reason != reasonEnvrc {
		t.Errorf(".envrc 的 Reason = %q，want %q", m[".envrc"].Reason, reasonEnvrc)
	}
	if m[".vscode/tasks.json"].Reason != reasonVSCode {
		t.Errorf(".vscode/tasks.json 的 Reason = %q，want %q", m[".vscode/tasks.json"].Reason, reasonVSCode)
	}
	if m[".devcontainer/devcontainer.json"].Reason != reasonDevCont {
		t.Errorf(".devcontainer/devcontainer.json 的 Reason = %q，want %q", m[".devcontainer/devcontainer.json"].Reason, reasonDevCont)
	}
	if m["AGENTS.md"].Reason != reasonAgentFile {
		t.Errorf("AGENTS.md 的 Reason = %q，want %q", m["AGENTS.md"].Reason, reasonAgentFile)
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
	for _, p := range []string{"run/hash.txt", "src/main.go", "README.md", ".vscode/settings.json"} {
		if _, ok := m[p]; ok {
			t.Errorf("%s 不应产生结果：%+v", p, m[p])
		}
	}
}

// TestExitModifiedExecutable 检查意见 7：把文本文件替换成可执行文件和新增一样危险，同样按文件头判断。
// 删除的改动读不到文件头，不产生结果。
func TestExitModifiedExecutable(t *testing.T) {
	files := t.TempDir()
	writeFile(t, files, "bin/tool", "\x7fELFfake")
	got := (Checker{}).Exit(t.TempDir(), files, []staging.Change{
		{Path: "bin/tool", Kind: staging.Modified},
		{Path: "bin/tool", Kind: staging.Deleted},
	})
	if len(got) != 1 || got[0].Kind != cabin.HighRisk || got[0].Reason != reasonExecutable {
		t.Errorf("Exit = %+v，want 只有 Modified 的可执行文件标为 high-risk", got)
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
		got := (Checker{}).Exit(t.TempDir(), files, []staging.Change{{Path: p, Kind: staging.Added}})
		if len(got) != 1 || got[0].Kind != cabin.HighRisk || got[0].Reason != reasonBadPath {
			t.Errorf("Exit(%q) = %+v，want 路径异常的 HighRisk，并且不读取文件", p, got)
		}
	}
}

// TestExitSymlinkDir 检查拒绝路径（红线 6、AC-8）：中间目录是符号链接时，读取不逃出暂存目录。
// 旧实现用 Lstat 加 Open，会跟随中间的符号链接，读到暂存区外的文件头。
func TestExitSymlinkDir(t *testing.T) {
	files := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "evil.sh", "\x7fELFfake")
	if err := os.Symlink(outside, filepath.Join(files, "run")); err != nil {
		t.Skipf("本机不能建立符号链接，跳过：%v", err)
	}
	got := (Checker{}).Exit(t.TempDir(), files, []staging.Change{{Path: "run/evil.sh", Kind: staging.Added}})
	if len(got) != 0 {
		t.Errorf("逃出暂存目录的符号链接不应产生结果：%+v", got)
	}
}

// TestExitSpecial 检查拒绝路径（红线 6、AC-8）：special 标记的文件和符号链接不按文件头判定。
func TestExitSpecial(t *testing.T) {
	files := t.TempDir()
	writeFile(t, files, "run/evil.sh", "\x7fELFfake")
	if got := (Checker{}).Exit(t.TempDir(), files, []staging.Change{{Path: "run/evil.sh", Kind: staging.Added, Special: true}}); len(got) != 0 {
		t.Errorf("special 文件不应产生结果：%+v", got)
	}

	outside := t.TempDir()
	writeFile(t, outside, "tool", "\x7fELFfake")
	if err := os.Symlink(filepath.Join(outside, "tool"), filepath.Join(files, "link.sh")); err != nil {
		t.Skipf("本机不能建立符号链接，跳过：%v", err)
	}
	if got := (Checker{}).Exit(t.TempDir(), files, []staging.Change{{Path: "link.sh", Kind: staging.Added}}); len(got) != 0 {
		t.Errorf("符号链接不应产生结果：%+v", got)
	}
}

// TestEntryGitConfig 检查 AC-2（Issue #74）：远程地址带令牌的 Git 配置有提示，
// 包括子模块的 .git/modules/*/config。不带令牌的没有提示。.git 中的其他文件
// 始终不产生结果。令牌用 fake- 前缀。
func TestEntryGitConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".git/config", "[remote \"origin\"]\n\turl = https://user:fake-token@github.com/o/r.git\n")
	writeFile(t, dir, ".git/objects/ab/cdef", "fake")
	writeFile(t, dir, "sub/.git/config", "[remote \"origin\"]\n\turl = https://fake-ghp-token@github.com/o/r.git\n")
	writeFile(t, dir, ".git/modules/dep/config", "[remote \"origin\"]\n\turl = https://user:fake-token@github.com/o/dep.git\n")
	got, err := (Checker{}).Entry(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := byPath(got)
	for _, p := range []string{".git/config", "sub/.git/config", ".git/modules/dep/config"} {
		if m[p].Kind != cabin.Notice || m[p].Reason != reasonGitCred {
			t.Errorf("%s = %+v，want notice（远程地址带令牌）", p, m[p])
		}
	}
	if _, ok := m[".git/objects/ab/cdef"]; ok {
		t.Errorf(".git 中的其他文件不应产生结果")
	}

	clean := t.TempDir()
	writeFile(t, clean, ".git/config", "[remote \"origin\"]\n\turl = https://github.com/o/r.git\n\turl = ssh://git@github.com/o/r.git\n")
	writeFile(t, clean, "a.txt", "fake")
	got, err = (Checker{}).Entry(clean)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("不带令牌的 .git/config 不应产生结果：%+v", got)
	}
}

// TestRewrite 检查 Git 远程配置的改写：去掉 http(s) 地址中的用户信息，保留 ssh 地址。
// .git/config 和子模块的 .git/modules/*/config 都改写。
func TestRewrite(t *testing.T) {
	if (Checker{}).Rewrite("README.md") != nil {
		t.Fatal("README.md 不应改写")
	}
	if (Checker{}).Rewrite("config") != nil {
		t.Fatal("不在 .git 下的 config 不应改写")
	}
	if (Checker{}).Rewrite(".git/modules/dep/config") == nil {
		t.Fatal("子模块的 .git/modules/*/config 应改写")
	}
	fn := (Checker{}).Rewrite(".Git/Config")
	if fn == nil {
		t.Fatal(".git/config 应返回改写函数")
	}
	in := "[remote \"o\"]\n\turl = https://user:fake-token@github.com/o/r.git\n" +
		"[remote \"p\"]\n\turl = ssh://git@github.com/p/r.git\n" +
		"[remote \"q\"]\n\turl = https://fake-ghp-token@github.com/q/r.git\n"
	want := "[remote \"o\"]\n\turl = https://github.com/o/r.git\n" +
		"[remote \"p\"]\n\turl = ssh://git@github.com/p/r.git\n" +
		"[remote \"q\"]\n\turl = https://github.com/q/r.git\n"
	if got := string(fn([]byte(in))); got != want {
		t.Errorf("改写结果不对：\n%s\nwant：\n%s", got, want)
	}
}

// TestExitPackageJSON 检查 AC-2（Issue #75）：只改 version 不标，
// 新增或修改安装生命周期脚本才标。基线读不到时宁可多标。
func TestExitPackageJSON(t *testing.T) {
	const baseJSON = `{"version":"1.0.0","scripts":{"test":"echo fake","postinstall":"echo base"}}`
	const versionOnly = `{"version":"2.0.0","scripts":{"test":"echo fake","postinstall":"echo base"}}`
	const scriptChanged = `{"version":"1.0.0","scripts":{"test":"echo fake","postinstall":"echo changed"}}`
	cases := []struct {
		name     string
		base     string // 项目中的 package.json；空串表示不存在
		staged   string // 暂存区中的 package.json；空串表示不存在
		kind     staging.Kind
		highRisk bool
	}{
		{"新增且带安装脚本", "", `{"scripts":{"postinstall":"echo fake"}}`, staging.Added, true},
		{"新增没有安装脚本", "", `{"scripts":{"test":"echo fake"}}`, staging.Added, false},
		{"只改 version 字段", baseJSON, versionOnly, staging.Modified, false},
		{"修改 postinstall", baseJSON, scriptChanged, staging.Modified, true},
		{"新增 preinstall", baseJSON, `{"scripts":{"test":"echo fake","postinstall":"echo base","preinstall":"echo fake"}}`, staging.Modified, true},
		{"新增 prepublish", baseJSON, `{"scripts":{"test":"echo fake","postinstall":"echo base","prepublish":"echo fake"}}`, staging.Modified, true},
		{"删掉 postinstall", baseJSON, `{"scripts":{"test":"echo fake"}}`, staging.Modified, false},
		{"基线缺失时按没有脚本处理", "", scriptChanged, staging.Modified, true},
		{"基线不是合法 JSON", "not json", scriptChanged, staging.Modified, true},
		// 暂存区读不了或解析不了，直接标高风险：截断或畸形的内容里可能藏着安装脚本。
		{"暂存区不是合法 JSON", baseJSON, "not json", staging.Modified, true},
		{"混入非字符串值不误报", baseJSON, `{"version":"1.0.0","scripts":{"test":"echo fake","postinstall":"echo base","junk":1}}`, staging.Modified, false},
		{"混入非字符串值且改了安装脚本", baseJSON, `{"version":"1.0.0","scripts":{"test":"echo fake","postinstall":"echo changed","junk":1}}`, staging.Modified, true},
		{"删除", baseJSON, "", staging.Deleted, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, files := t.TempDir(), t.TempDir()
			if tc.base != "" {
				writeFile(t, project, "package.json", tc.base)
			}
			if tc.staged != "" {
				writeFile(t, files, "package.json", tc.staged)
			}
			got := (Checker{}).Exit(project, files, []staging.Change{{Path: "package.json", Kind: tc.kind}})
			if tc.highRisk {
				if len(got) != 1 || got[0].Kind != cabin.HighRisk || got[0].Reason != reasonLifecycle {
					t.Fatalf("Exit = %+v，want high-risk（安装脚本有变化）", got)
				}
			} else if len(got) != 0 {
				t.Fatalf("Exit = %+v，want 无结果", got)
			}
		})
	}
}

// TestExitPackageJSONNested 检查嵌套的 package.json（npm workspaces）同样判定，大小写变体也算。
func TestExitPackageJSONNested(t *testing.T) {
	project, files := t.TempDir(), t.TempDir()
	writeFile(t, project, "packages/lib/Package.json", `{"scripts":{"test":"echo fake"}}`)
	writeFile(t, files, "packages/lib/Package.json", `{"scripts":{"test":"echo fake","prepare":"echo fake"}}`)
	got := (Checker{}).Exit(project, files, []staging.Change{{Path: "packages/lib/Package.json", Kind: staging.Modified}})
	if len(got) != 1 || got[0].Kind != cabin.HighRisk {
		t.Fatalf("Exit = %+v，want high-risk（嵌套 package.json 新增 prepare）", got)
	}
}

// TestExitPackageJSONOversize 检查超过读取上限的 package.json 直接标高风险（红线 6）。
func TestExitPackageJSONOversize(t *testing.T) {
	project, files := t.TempDir(), t.TempDir()
	writeFile(t, project, "package.json", `{"scripts":{"test":"echo fake"}}`)
	writeFile(t, files, "package.json", `{"scripts":{"postinstall":"echo fake"},"pad":"`+strings.Repeat("a", pkgLimit)+`"}`)
	got := (Checker{}).Exit(project, files, []staging.Change{{Path: "package.json", Kind: staging.Modified}})
	if len(got) != 1 || got[0].Kind != cabin.HighRisk {
		t.Fatalf("Exit = %+v，want high-risk（超过读取上限）", got)
	}
}

// 目录不存在或文件读不了时，各项嗅探按"没有命中"处理，不报错，也不越出目录。
func TestSniffUnreadable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if isExecutable(missing, "a") {
		t.Error("isExecutable：不存在的目录应返回 false")
	}
	if _, ok := packageScripts(missing, "package.json"); ok {
		t.Error("packageScripts：不存在的目录应返回 false")
	}

	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"scripts":{"postinstall":"fake"}}`)
	writeFile(t, dir, "config", "url = https://fake-user:fake-token@example.com/r.git\n")
	writeFile(t, dir, "id_fake", "-----BEGIN OPENSSH PRIVATE KEY-----\n")
	writeFile(t, dir, "run.sh", "#!/bin/sh\n")
	for _, f := range []string{"package.json", "config", "id_fake", "run.sh"} {
		p := filepath.Join(dir, f)
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(p, 0o644) })
	}
	if f, err := os.Open(filepath.Join(dir, "config")); err == nil {
		f.Close()
		t.Skip("这个环境不受文件权限限制")
	}
	if hasCredURL(filepath.Join(dir, "config")) {
		t.Error("hasCredURL：读不了的文件应返回 false")
	}
	if hasPrivateKeyHead(filepath.Join(dir, "id_fake")) {
		t.Error("hasPrivateKeyHead：读不了的文件应返回 false")
	}
	if isExecutable(dir, "run.sh") {
		t.Error("isExecutable：读不了的文件应返回 false")
	}
	if _, ok := packageScripts(dir, "package.json"); ok {
		t.Error("packageScripts：读不了的文件应返回 false")
	}
}
