package staging

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stripAuth 模拟进舱改写：去掉 .git/config 远程地址中的用户信息。令牌用 fake- 前缀。
func stripAuth(rel string) func(content []byte) []byte {
	if rel != ".git/config" {
		return nil
	}
	return func(content []byte) []byte {
		return []byte(strings.ReplaceAll(string(content), "user:fake-token@", ""))
	}
}

const (
	gitConfigWithToken = "[remote \"origin\"]\n\turl = https://user:fake-token@github.com/o/r.git\n"
	gitConfigClean     = "[remote \"origin\"]\n\turl = https://github.com/o/r.git\n"
	agentConfig        = "[core]\n\tbare = true\n"
)

// newRewrittenArea 建立带令牌的项目和改写后的暂存区。
func newRewrittenArea(t *testing.T) (project string, a *Area) {
	t.Helper()
	project = t.TempDir()
	writeTree(t, project, map[string]string{
		".git/config": gitConfigWithToken,
		"a.txt":       "fake-a",
	})
	a, err := Create(project, filepath.Join(t.TempDir(), "area"), nil, stripAuth, nil)
	if err != nil {
		t.Fatal(err)
	}
	return project, a
}

// TestCreateRewrites 检查改写落地：暂存区中没有令牌，基线是改写后内容的哈希，改写记录入清单。
func TestCreateRewrites(t *testing.T) {
	_, a := newRewrittenArea(t)
	got := readTree(t, a.Files())
	if got[".git/config"] != gitConfigClean {
		t.Errorf(".git/config = %q，want %q", got[".git/config"], gitConfigClean)
	}
	if got["a.txt"] != "fake-a" {
		t.Errorf("a.txt = %q，改写不应影响其他文件", got["a.txt"])
	}
	var m manifest
	data, err := os.ReadFile(filepath.Join(a.dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Rewritten[".git/config"]; !ok {
		t.Errorf("manifest = %v，rewritten 缺少 .git/config", m)
	}
	if m.Base[".git/config"] != hash([]byte(gitConfigClean)) {
		t.Errorf("base 哈希不是改写后内容的哈希")
	}
}

// TestChangesRewritten 检查改写不产生改动，agent 改动后不误报冲突。
func TestChangesRewritten(t *testing.T) {
	_, a := newRewrittenArea(t)
	if cs, err := a.Changes(); err != nil || len(cs) != 0 {
		t.Fatalf("Changes = %v, %v，改写不应产生改动", cs, err)
	}
	writeTree(t, a.Files(), map[string]string{".git/config": agentConfig})
	cs, err := a.Changes()
	if err != nil || len(cs) != 1 || cs[0].Path != ".git/config" || cs[0].Kind != Modified || cs[0].Conflict {
		t.Fatalf("Changes = %v, %v，项目未变时不应标冲突", cs, err)
	}
}

// TestRevertRewritten 检查撤销：恢复的是改写后的内容，令牌不回到暂存区。
func TestRevertRewritten(t *testing.T) {
	_, a := newRewrittenArea(t)
	writeTree(t, a.Files(), map[string]string{".git/config": agentConfig})
	if err := a.Revert(".git/config"); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if got := readTree(t, a.Files())[".git/config"]; got != gitConfigClean {
		t.Fatalf("撤销后 .git/config = %q，want %q", got, gitConfigClean)
	}
	if cs, err := a.Changes(); err != nil || len(cs) != 0 {
		t.Fatalf("Changes = %v, %v，撤销后不应有改动", cs, err)
	}
}

// TestRewrittenConflict 检查拒绝路径：项目中的同名文件在运行期间被修改时，撤销和写回都拒绝。
func TestRewrittenConflict(t *testing.T) {
	project, a := newRewrittenArea(t)
	writeTree(t, a.Files(), map[string]string{".git/config": agentConfig})
	writeTree(t, project, map[string]string{".git/config": "[core]\n\tbare = false\n"})
	cs, err := a.Changes()
	if err != nil || len(cs) != 1 || !cs[0].Conflict {
		t.Fatalf("Changes = %v, %v，项目变更后应标冲突", cs, err)
	}
	if err := a.Revert(".git/config"); !errors.Is(err, errConflict) {
		t.Errorf("Revert = %v，want errConflict", err)
	}
	if err := a.Apply(".git/config"); !errors.Is(err, errConflict) {
		t.Errorf("Apply = %v，want errConflict", err)
	}
}

// TestRewrittenAfterOpen 检查程序重启后的行为：改写记录仍在，撤销仍恢复改写后的内容。
func TestRewrittenAfterOpen(t *testing.T) {
	project, a := newRewrittenArea(t)
	writeTree(t, a.Files(), map[string]string{".git/config": agentConfig})
	b, err := Open(project, a.dir, stripAuth)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := b.Changes()
	if err != nil || len(cs) != 1 || cs[0].Kind != Modified || cs[0].Conflict {
		t.Fatalf("重启后 Changes = %v, %v", cs, err)
	}
	if err := b.Revert(".git/config"); err != nil {
		t.Fatalf("重启后 Revert: %v", err)
	}
	if got := readTree(t, b.Files())[".git/config"]; got != gitConfigClean {
		t.Fatalf("重启后撤销 .git/config = %q，want %q", got, gitConfigClean)
	}
	// 打开时没有改写函数，撤销改写过的文件要报错，不能把令牌复制回暂存区。
	writeTree(t, b.Files(), map[string]string{".git/config": agentConfig})
	c, err := Open(project, a.dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Revert(".git/config"); err == nil {
		t.Fatal("没有改写函数时，Revert 应返回错误")
	}
	if got := readTree(t, c.Files())[".git/config"]; strings.Contains(got, "fake-token") {
		t.Fatal("令牌不应回到暂存区")
	}
}

// TestOpenLegacyManifest 检查旧版基线清单（路径到哈希的扁平映射）仍能打开。
func TestOpenLegacyManifest(t *testing.T) {
	project := t.TempDir()
	writeTree(t, project, map[string]string{"a.txt": "fake-a"})
	dir := filepath.Join(t.TempDir(), "area")
	a, err := Create(project, dir, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	flat, err := json.Marshal(a.base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), flat, 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(project, dir, nil)
	if err != nil {
		t.Fatalf("旧格式基线清单应能打开：%v", err)
	}
	if cs, err := b.Changes(); err != nil || len(cs) != 0 {
		t.Fatalf("Changes = %v, %v", cs, err)
	}
}

// agent 把改写过的文件换成了目录。撤销时删除目录，恢复改写后的内容。
func TestRevertRewrittenOverDir(t *testing.T) {
	_, a := newRewrittenArea(t)
	p := filepath.Join(a.Files(), ".git", "config")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	writeTree(t, a.Files(), map[string]string{".git/config/x": "fake-x"})
	if err := a.Revert(".git/config"); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if got := readTree(t, a.Files())[".git/config"]; got != gitConfigClean {
		t.Fatalf("撤销后 .git/config = %q，want %q", got, gitConfigClean)
	}
}

// 要改写的文件读不了时，Create 报错，不留下暂存区。
func TestCreateUnreadableRewritten(t *testing.T) {
	project := t.TempDir()
	writeTree(t, project, map[string]string{".git/config": gitConfigWithToken})
	unreadable(t, filepath.Join(project, ".git", "config"))
	dir := filepath.Join(t.TempDir(), "area")
	if _, err := Create(project, dir, nil, stripAuth, nil); err == nil {
		t.Fatal("要改写的文件读不了时 Create 没有报错")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("Create 失败后留下了暂存区")
	}
}

// 撤销改写过的文件时，项目中的原文件读不了，或者暂存区的目录不能写入，Revert 都报错。
func TestRevertRewrittenFails(t *testing.T) {
	t.Run("项目中的原文件读不了", func(t *testing.T) {
		project, a := newRewrittenArea(t)
		writeTree(t, a.Files(), map[string]string{".git/config": agentConfig})
		unreadable(t, filepath.Join(project, ".git", "config"))
		if err := a.Revert(".git/config"); err == nil {
			t.Fatal("Revert 没有报错")
		}
	})
	t.Run("暂存区的目录不能写入", func(t *testing.T) {
		_, a := newRewrittenArea(t)
		writeTree(t, a.Files(), map[string]string{".git/config": agentConfig})
		dir := filepath.Join(a.Files(), ".git")
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("这个环境不受文件权限限制")
		}
		os.Chmod(dir, 0o555)
		t.Cleanup(func() { os.Chmod(dir, 0o755) })
		if err := a.Revert(".git/config"); err == nil {
			t.Fatal("Revert 没有报错")
		}
		if got := readTree(t, a.Files())[".git/config"]; got != agentConfig {
			t.Errorf("Revert 失败后 .git/config = %q，want 保持 agent 的内容", got)
		}
	})
}
