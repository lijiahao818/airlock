package staging

import (
	"os"
	"path/filepath"
	"testing"
)

// 项目文件夹被删除后，各个操作都返回错误。
func TestMissingProject(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2"})
	if err := os.RemoveAll(project); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Changes(); err == nil {
		t.Error("项目不存在时 Changes 没有报错")
	}
	if err := a.Apply("a.txt"); err == nil {
		t.Error("项目不存在时 Apply 没有报错")
	}
	if err := a.Revert("a.txt"); err == nil {
		t.Error("项目不存在时 Revert 没有报错")
	}
}

func TestMissingFiles(t *testing.T) {
	project, a := newArea(t)
	if err := os.RemoveAll(a.Files()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Changes(); err == nil {
		t.Error("暂存区不存在时 Changes 没有报错")
	}
	if _, err := Open(project, a.dir); err == nil {
		t.Error("暂存区不存在时 Open 没有报错")
	}
}

func TestOpenRejectsBadManifest(t *testing.T) {
	project, a := newArea(t)
	if err := os.WriteFile(filepath.Join(a.dir, "manifest.json"), []byte("fake-not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(project, a.dir); err == nil {
		t.Fatal("基线清单格式错误时 Open 没有报错")
	}
}

// Apply 写回时，项目中的父路径是普通文件，无法建目录，必须报错并保持文件不变。
func TestApplyParentIsFile(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"x/y.txt": "fake-y"})
	writeTree(t, project, map[string]string{"x": "fake-file"})
	if err := a.Apply("x/y.txt"); err == nil {
		t.Fatal("父路径是文件时 Apply 没有报错")
	}
	if got := readTree(t, project)["x"]; got != "fake-file" {
		t.Errorf("项目中的 x 被改动：%q", got)
	}
}
