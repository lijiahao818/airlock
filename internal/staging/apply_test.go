package staging

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestApplyWritesBack(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2", "new/deep/c.txt": "fake-c"})
	os.Remove(filepath.Join(a.Files(), "sub", "b.txt"))
	for _, rel := range []string{"a.txt", "new/deep/c.txt", "sub/b.txt"} {
		if err := a.Apply(rel); err != nil {
			t.Fatalf("Apply(%s): %v", rel, err)
		}
	}
	want := map[string]string{
		"a.txt":          "fake-a2",
		"new/deep/c.txt": "fake-c",
		".git/config":    "fake-git-config",
		".env":           "FAKE_SECRET=fake-value",
	}
	if got := readTree(t, project); !reflect.DeepEqual(got, want) {
		t.Fatalf("项目内容 %v\nwant %v", got, want)
	}
	// 写回后项目已经不是基线，同一个改动不能再写一次。
	if err := a.Apply("a.txt"); !errors.Is(err, errConflict) {
		t.Errorf("再次 Apply = %v，want errConflict", err)
	}
}

// 每条拒绝路径都不能改动项目。
func TestApplyRejects(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2", "new.txt": "fake-new"})
	writeTree(t, project, map[string]string{"new.txt": "fake-new-by-user"})
	os.Remove(filepath.Join(a.Files(), "sub", "b.txt"))
	os.Mkdir(filepath.Join(a.Files(), "sub", "b.txt"), 0o755) // 文件被换成目录，属于特殊改动
	before := readTree(t, project)

	cases := map[string]error{
		"new.txt":      errConflict,
		"sub/b.txt":    errSpecial,
		".git/config":  errNoChange,
		"":             errBadPath,
		".":            errBadPath,
		"../a.txt":     errBadPath,
		"sub/../a.txt": errBadPath,
		"/a.txt":       errBadPath,
		"a.txt/":       errBadPath,
		"sub//b.txt":   errBadPath,
		`sub\b.txt`:    errBadPath,
		`C:\a.txt`:     errBadPath,
		"a.txt:stream": errBadPath,
		"fake-missing": errNoChange,
	}
	for rel, want := range cases {
		if err := a.Apply(rel); !errors.Is(err, want) {
			t.Errorf("Apply(%q) = %v，want %v", rel, err, want)
		}
	}
	if got := readTree(t, project); !reflect.DeepEqual(got, before) {
		t.Fatalf("拒绝的 Apply 改动了项目：%v", got)
	}
}

// 项目中的目录是指向项目以外的链接时，Apply 不能经过它写到项目以外。
func TestApplyDoesNotFollowProjectLink(t *testing.T) {
	project, a := newArea(t)
	outside := t.TempDir()
	dirLink(t, outside, filepath.Join(project, "out"))
	stage(t, a, map[string]string{"out/evil.txt": "fake-evil"})
	if err := a.Apply("out/evil.txt"); err == nil {
		t.Error("Apply 经过链接写入了项目以外")
	}
	if got := readTree(t, outside); len(got) != 0 {
		t.Fatalf("项目以外出现了文件：%v", got)
	}
}

// Apply 写完后不留下临时文件，也不删除项目中名字相近的文件。
func TestApplyLeavesNoTempFile(t *testing.T) {
	project, a := newArea(t)
	writeTree(t, project, map[string]string{"a.txt.airlock-tmp": "fake-user-file"})
	stage(t, a, map[string]string{"a.txt": "fake-a2"})
	if err := a.Apply("a.txt"); err != nil {
		t.Fatal(err)
	}
	for rel, content := range readTree(t, project) {
		if strings.Contains(rel, ".airlock-") && content != "fake-user-file" {
			t.Errorf("留下了临时文件 %s", rel)
		}
	}
	if got := readTree(t, project)["a.txt.airlock-tmp"]; got != "fake-user-file" {
		t.Error("Apply 改动了用户的文件")
	}
}
