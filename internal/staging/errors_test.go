package staging

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

func TestCreateDirUnderFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "fake-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(t.TempDir(), filepath.Join(file, "area"), nil, nil); err == nil {
		t.Fatal("暂存区的上级是文件时 Create 没有报错")
	}
}

// Create 失败时只删除自己创建的内容，dir 中原有的文件保留。
func TestCreateKeepsExistingDir(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"fake-other.txt": "fake-keep"})
	if _, err := Create(filepath.Join(t.TempDir(), "fake-missing"), dir, nil, nil); err == nil {
		t.Fatal("项目不存在时 Create 没有报错")
	}
	if got := readTree(t, dir); len(got) != 1 || got["fake-other.txt"] != "fake-keep" {
		t.Fatalf("Create 失败后 dir 的内容 %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "files")); !os.IsNotExist(err) {
		t.Error("Create 失败后留下了 files 目录")
	}
}

// 改名失败时，Apply 删除临时文件。Windows 上目标文件被打开时改名失败，其他系统跳过。
func TestApplyRenameFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只有 Windows 在目标文件被打开时拒绝改名")
	}
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2"})
	f, err := os.Open(filepath.Join(project, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	err = a.Apply("a.txt")
	f.Close()
	if err == nil {
		t.Skip("这个系统允许改名到打开的文件")
	}
	if got := readTree(t, project); !reflect.DeepEqual(got, baseProject) {
		t.Fatalf("改名失败后项目内容 %v，want 原来的内容", got)
	}
}

// unreadable 让 p 不能读取，测试结束时恢复。Windows 和 root 用户不受权限限制，跳过测试。
func unreadable(t *testing.T, p string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("这个环境不受文件权限限制")
	}
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o755) })
}

func TestCreateUnreadableProject(t *testing.T) {
	for _, rel := range []string{"a.txt", "sub"} {
		project := t.TempDir()
		writeTree(t, project, baseProject)
		unreadable(t, filepath.Join(project, filepath.FromSlash(rel)))
		dir := filepath.Join(t.TempDir(), "area")
		if _, err := Create(project, dir, nil, nil); err == nil {
			t.Errorf("%s 不能读取时 Create 没有报错", rel)
		}
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("%s 不能读取时 Create 留下了暂存区", rel)
		}
	}
}

// 暂存区中的文件不能读取时，各个操作都返回错误，不改动项目。
func TestUnreadableStagingFile(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2"})
	unreadable(t, filepath.Join(a.Files(), "a.txt"))
	if _, err := a.Changes(); err == nil {
		t.Error("a.txt 不能读取时 Changes 没有报错")
	}
	if err := a.Apply("a.txt"); err == nil {
		t.Error("a.txt 不能读取时 Apply 没有报错")
	}
	if err := a.Revert("a.txt"); err == nil {
		t.Error("a.txt 不能读取时 Revert 没有报错")
	}
	if got := readTree(t, project)["a.txt"]; got != "fake-a" {
		t.Errorf("项目中的 a.txt 被改动：%q", got)
	}
}

// 暂存区中的目录不能读取时，Changes 无法遍历，返回错误。
func TestUnreadableStagingDir(t *testing.T) {
	_, a := newArea(t)
	unreadable(t, filepath.Join(a.Files(), "sub"))
	if _, err := a.Changes(); err == nil {
		t.Error("sub 不能读取时 Changes 没有报错")
	}
}

// 项目中的文件不能读取时，按冲突处理，Apply 不写回。
func TestUnreadableProjectFile(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2"})
	unreadable(t, filepath.Join(project, "a.txt"))
	if _, err := a.Changes(); err == nil {
		t.Fatal("项目中的文件不能读取时 Changes 没有报错")
	}
	if err := a.Apply("a.txt"); err == nil {
		t.Fatal("项目中的文件不能读取时 Apply 没有报错")
	}
}

// 项目中的目录不能写入时，Apply 报错，并且不留下临时文件。
func TestApplyReadOnlyProject(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"sub/b.txt": "fake-b2"})
	os.Remove(filepath.Join(a.Files(), "a.txt"))
	sub := filepath.Join(project, "sub")
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("这个环境不受文件权限限制")
	}
	os.Chmod(sub, 0o555)
	os.Chmod(project, 0o555)
	t.Cleanup(func() { os.Chmod(sub, 0o755); os.Chmod(project, 0o755) })
	if err := a.Apply("sub/b.txt"); err == nil {
		t.Error("目录不能写入时 Apply 没有报错")
	}
	if err := a.Apply("a.txt"); err == nil {
		t.Error("目录不能写入时删除文件的 Apply 没有报错")
	}
	if got := readTree(t, project); got["sub/b.txt"] != "fake-b" || got["a.txt"] != "fake-a" || len(got) != len(baseProject) {
		t.Errorf("项目被改动：%v", got)
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
