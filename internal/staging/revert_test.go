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

// 进舱检查排除的文件不在基线清单中。撤销只删除暂存区中的同名文件，不从项目复制。
func TestRevertNeverCopiesExcludedFile(t *testing.T) {
	_, a := newArea(t)
	if err := a.Revert(".env"); !errors.Is(err, errNoChange) {
		t.Errorf("Revert(.env) = %v，want errNoChange", err)
	}
	stage(t, a, map[string]string{".env": "fake-by-agent"})
	if err := a.Revert(".env"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(a.Files(), ".env")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("撤销后暂存区中有 .env")
	}
}

func TestRevertRejects(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2", "sub/b.txt": "fake-b-by-user"})
	// 项目中的 a.txt 和 sub/b.txt 在运行期间被修改。sub/b.txt 与暂存区相同，所以不算冲突。
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

// 新增的文件在项目中有同名文件时，撤销只删除暂存区中的文件，项目不变。
func TestRevertAddedConflict(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"new.txt": "fake-new"})
	writeTree(t, project, map[string]string{"new.txt": "fake-new-by-user"})
	if err := a.Revert("new.txt"); err != nil {
		t.Fatal(err)
	}
	if got := changes(t, a); len(got) != 0 {
		t.Errorf("撤销后仍有改动：%+v", got)
	}
	if got := readTree(t, project)["new.txt"]; got != "fake-new-by-user" {
		t.Errorf("撤销改动了项目中的文件：%q", got)
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
