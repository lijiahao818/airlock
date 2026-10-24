package staging

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRevertRestoresBaseline(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2", "new.txt": "fake-new"})
	os.Remove(filepath.Join(a.Files(), "sub", "b.txt"))
	for _, rel := range []string{"a.txt", "new.txt", "sub/b.txt"} {
		if err := a.Revert(rel); err != nil {
			t.Fatalf("Revert(%s): %v", rel, err)
		}
	}
	if got := changes(t, a); len(got) != 0 {
		t.Errorf("撤销后仍有改动：%+v", got)
	}
	if got := readTree(t, project); !reflect.DeepEqual(got, baseProject) {
		t.Errorf("Revert 改动了项目：%v", got)
	}
}

// 文件被换成目录或链接时，撤销后恢复为原来的文件。
func TestRevertSpecial(t *testing.T) {
	_, a := newArea(t)
	os.Remove(filepath.Join(a.Files(), "a.txt"))
	os.MkdirAll(filepath.Join(a.Files(), "a.txt", "inner"), 0o755)
	outside := t.TempDir()
	dirLink(t, outside, filepath.Join(a.Files(), "link"))
	for _, rel := range []string{"a.txt", "link"} {
		if err := a.Revert(rel); err != nil {
			t.Fatalf("Revert(%s): %v", rel, err)
		}
	}
	if got := changes(t, a); len(got) != 0 {
		t.Errorf("撤销后仍有改动：%+v", got)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("撤销链接时删除了链接的目标")
	}
}

// 进舱检查排除的文件不在基线清单中，Revert 不会把它从项目复制进暂存区。
// agent 新建的 .env 与项目中的 .env 冲突，Revert 拒绝，暂存区中仍是 agent 写的内容。
func TestRevertNeverCopiesExcludedFile(t *testing.T) {
	_, a := newArea(t)
	if err := a.Revert(".env"); !errors.Is(err, errNoChange) {
		t.Errorf("Revert(.env) = %v，want errNoChange", err)
	}
	if _, err := os.Lstat(filepath.Join(a.Files(), ".env")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Revert 把 .env 复制进了暂存区")
	}
	stage(t, a, map[string]string{".env": "fake-by-agent"})
	if err := a.Revert(".env"); !errors.Is(err, errConflict) {
		t.Errorf("Revert(agent 的 .env) = %v，want errConflict", err)
	}
	if got := readTree(t, a.Files())[".env"]; got != "fake-by-agent" {
		t.Fatalf("暂存区中的 .env = %q，want agent 写的内容", got)
	}
}

func TestRevertRejects(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2", "sub/b.txt": "fake-b-by-user"})
	// 项目中的 a.txt 和 sub/b.txt 在运行期间被修改。sub/b.txt 与暂存区内容相同，仍然算冲突。
	writeTree(t, project, map[string]string{"a.txt": "fake-a-by-user", "sub/b.txt": "fake-b-by-user"})
	before := readTree(t, a.Files())

	cases := map[string]error{
		"a.txt":        errConflict,
		"sub/b.txt":    errConflict, // 项目已经不是基线，复制回去不能恢复基线
		".git/config":  errNoChange,
		"":             errBadPath,
		".":            errBadPath,
		"../a.txt":     errBadPath,
		"/a.txt":       errBadPath,
		`sub\b.txt`:    errBadPath,
		"fake-missing": errNoChange,
	}
	for rel, want := range cases {
		if err := a.Revert(rel); !errors.Is(err, want) {
			t.Errorf("Revert(%q) = %v，want %v", rel, err, want)
		}
	}
	if got := readTree(t, a.Files()); !reflect.DeepEqual(got, before) {
		t.Fatalf("拒绝的 Revert 改动了暂存区：%v", got)
	}
}

// 新增的文件在项目中有同名文件时，改动冲突，Revert 拒绝，暂存区和项目都不变。
func TestRevertAddedConflict(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"new.txt": "fake-new"})
	writeTree(t, project, map[string]string{"new.txt": "fake-new-by-user"})
	if err := a.Revert("new.txt"); !errors.Is(err, errConflict) {
		t.Fatalf("Revert = %v，want errConflict", err)
	}
	if got := readTree(t, a.Files())["new.txt"]; got != "fake-new" {
		t.Errorf("拒绝的 Revert 改动了暂存区：%q", got)
	}
	if got := readTree(t, project)["new.txt"]; got != "fake-new-by-user" {
		t.Errorf("拒绝的 Revert 改动了项目：%q", got)
	}
}

// 暂存区中的目录被换成指向别处的链接时，撤销不能经过链接改动暂存区以外的文件。
func TestRevertDoesNotFollowDirLink(t *testing.T) {
	_, a := newArea(t)
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"b.txt": "fake-outside"})
	os.RemoveAll(filepath.Join(a.Files(), "sub"))
	dirLink(t, outside, filepath.Join(a.Files(), "sub"))
	a.Revert("sub/b.txt")
	if got := readTree(t, outside); !reflect.DeepEqual(got, map[string]string{"b.txt": "fake-outside"}) {
		t.Fatalf("Revert 改动了暂存区以外的文件：%v", got)
	}
}
