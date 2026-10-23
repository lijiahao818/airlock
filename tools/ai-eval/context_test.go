package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCollectContext(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	write := func(name, body string) {
		p := filepath.Join(parent, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("repo/docs/a.md", "第一份文档")
	write("repo/docs/big.md", strings.Repeat("x", 100))
	write("repo/CLAUDE.md", "规则")
	write("secret.txt", "fake-secret")

	issues := "读 `docs/a.md`、`docs/a.md`、`./docs/a.md`、`docs`、`docs/missing.md`、" +
		"`../secret.txt`、`" + filepath.Join(parent, "secret.txt") + "`、`CLAUDE.md`、`docs/big.md`。"
	got, omitted := collectContext(issues, root, 50)

	if !strings.Contains(got, "===== docs/a.md =====\n第一份文档") {
		t.Errorf("没有包含 docs/a.md：\n%s", got)
	}
	if strings.Count(got, "=====") != 2 {
		t.Errorf("重复的路径应只读取一次：\n%s", got)
	}
	for _, bad := range []string{"fake-secret", "规则"} {
		if strings.Contains(got, bad) {
			t.Errorf("上下文中不应有 %q：\n%s", bad, got)
		}
	}
	if want := []string{"docs/big.md"}; !reflect.DeepEqual(omitted, want) {
		t.Errorf("omitted = %v, want %v", omitted, want)
	}
}

func TestCollectContextRejectsSymlinkOutsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("fake-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(parent, "secret.txt"), filepath.Join(root, "link.md")); err != nil {
		t.Skip("当前系统不能创建符号链接：", err)
	}
	if got, _ := collectContext("`link.md`", root, 1000); got != "" {
		t.Fatalf("读取了根目录以外的文件：%q", got)
	}
}

func TestCollectContextMissingRoot(t *testing.T) {
	if got, omitted := collectContext("`docs/a.md`", filepath.Join(t.TempDir(), "none"), 1000); got != "" || omitted != nil {
		t.Fatalf("got %q, %v", got, omitted)
	}
}
