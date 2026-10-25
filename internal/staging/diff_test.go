package staging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func diff(t *testing.T, a *Area, rel string) Diff {
	t.Helper()
	d, err := a.Diff(rel)
	if err != nil {
		t.Fatalf("Diff(%q): %v", rel, err)
	}
	return d
}

// 修改的文件显示新增和删除的行（#98 AC-1）。
func TestDiffModified(t *testing.T) {
	project := t.TempDir()
	writeTree(t, project, map[string]string{"m.txt": "l1\nl2\nl3\n"})
	a, err := Create(project, filepath.Join(t.TempDir(), "area"), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	stage(t, a, map[string]string{"m.txt": "l1\nfake-new\nl3\n"})
	want := []Line{{Op: OpSame, Text: "l1"}, {Op: OpDel, Text: "l2"}, {Op: OpAdd, Text: "fake-new"}, {Op: OpSame, Text: "l3"}}
	if got := diff(t, a, "m.txt"); got.Reason != "" || !reflect.DeepEqual(got.Lines, want) {
		t.Fatalf("Diff = %+v\nwant %+v", got, want)
	}
}

// 新增的文件显示全部内容，删除的文件显示原来的内容（#98 AC-2）。
func TestDiffAddedAndDeleted(t *testing.T) {
	_, a := newArea(t)
	stage(t, a, map[string]string{"new.txt": "fake-x\nfake-y"})
	os.Remove(filepath.Join(a.Files(), "sub", "b.txt"))
	if got, want := diff(t, a, "new.txt").Lines, []Line{{Op: OpAdd, Text: "fake-x"}, {Op: OpAdd, Text: "fake-y"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("新增文件 Diff = %+v\nwant %+v", got, want)
	}
	if got, want := diff(t, a, "sub/b.txt").Lines, []Line{{Op: OpDel, Text: "fake-b"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("删除文件 Diff = %+v\nwant %+v", got, want)
	}
}

// 二进制文件、不是 UTF-8 的文件和超过上限的文件不显示内容，只给出原因（#98 AC-4）。
func TestDiffRefusesContent(t *testing.T) {
	_, a := newArea(t)
	stage(t, a, map[string]string{
		"bin.dat":  "fake\x00bin",
		"gbk.txt":  "\xc4\xe3\xba\xc3",
		"big.txt":  strings.Repeat("x", maxDiffSize+1),
		"edge.txt": strings.Repeat("y", maxDiffSize),
	})
	for rel, want := range map[string]string{"bin.dat": reasonBinary, "gbk.txt": reasonBinary, "big.txt": reasonTooBig} {
		if got := diff(t, a, rel); got.Reason != want || len(got.Lines) != 0 {
			t.Errorf("Diff(%q) = %+v，want 原因 %q", rel, got, want)
		}
	}
	if got := diff(t, a, "edge.txt"); got.Reason != "" || len(got.Lines) != 1 {
		t.Errorf("正好 1 MB 的文件 Diff 原因 = %q，行数 %d", got.Reason, len(got.Lines))
	}
}

// 符号链接不读取内容，即使它指向暂存区以外的文件（#98 AC-4）。
func TestDiffSymlink(t *testing.T) {
	_, a := newArea(t)
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"secret.txt": "fake-secret"})
	symlink(t, filepath.Join(outside, "secret.txt"), filepath.Join(a.Files(), "link.txt"))
	if got := diff(t, a, "link.txt"); got.Reason != reasonSpecial || len(got.Lines) != 0 {
		t.Fatalf("Diff = %+v", got)
	}
}

// 经过目录链接越出暂存区的路径不读取内容（#98 AC-4、AC-5）。
func TestDiffDirLinkEscape(t *testing.T) {
	_, a := newArea(t)
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"secret.txt": "fake-secret"})
	dirLink(t, outside, filepath.Join(a.Files(), "out"))
	if got := diff(t, a, "out/secret.txt"); got.Reason != reasonSpecial || len(got.Lines) != 0 {
		t.Fatalf("Diff = %+v，want 不读取内容", got)
	}
}

// 不合法的路径和没有改动的文件被拒绝（#98 AC-5）。
func TestDiffRejects(t *testing.T) {
	_, a := newArea(t)
	cases := map[string]error{
		"../x":         errBadPath,
		"/etc/passwd":  errBadPath,
		`sub\b.txt`:    errBadPath,
		"C:/x":         errBadPath,
		"a.txt":        errNoChange,
		"fake-missing": errNoChange,
		"sub/../a.txt": errBadPath,
	}
	for rel, want := range cases {
		if _, err := a.Diff(rel); !errors.Is(err, want) {
			t.Errorf("Diff(%q) = %v，want %v", rel, err, want)
		}
	}
}

// 离改动远的不变行合并成 skip 行。
func TestDiffLinesCollapse(t *testing.T) {
	var old []string
	for i := range 20 {
		old = append(old, strings.Repeat("l", i+1))
	}
	cur := append([]string{}, old...)
	cur[10] = "fake-changed"
	got := diffLines(strings.Join(old, "\n")+"\n", strings.Join(cur, "\n")+"\n")
	want := []Line{{Op: OpSkip, Skip: 7}}
	for i := 7; i < 10; i++ {
		want = append(want, Line{Op: OpSame, Text: old[i]})
	}
	want = append(want, Line{Op: OpDel, Text: old[10]}, Line{Op: OpAdd, Text: "fake-changed"})
	for i := 11; i < 14; i++ {
		want = append(want, Line{Op: OpSame, Text: old[i]})
	}
	want = append(want, Line{Op: OpSkip, Skip: 6})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diffLines = %+v\nwant %+v", got, want)
	}
}

// 中间部分交错改动时，按最长公共子序列对齐。
func TestDiffLinesLCS(t *testing.T) {
	got := diffLines("a\nb\nc\nd\n", "a\nx\nc\ny\n")
	want := []Line{
		{Op: OpSame, Text: "a"}, {Op: OpDel, Text: "b"}, {Op: OpAdd, Text: "x"},
		{Op: OpSame, Text: "c"}, {Op: OpDel, Text: "d"}, {Op: OpAdd, Text: "y"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diffLines = %+v\nwant %+v", got, want)
	}
}

// 项目一侧的文件不能显示时，也只给出原因：二进制文件、被换成目录、父目录被换成指向项目以外的链接。
func TestDiffProjectSide(t *testing.T) {
	project := t.TempDir()
	writeTree(t, project, map[string]string{"bin.dat": "fake\x00bin", "dir.txt": "fake-d", "sub/x.txt": "fake-x"})
	a, err := Create(project, filepath.Join(t.TempDir(), "area"), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	stage(t, a, map[string]string{"bin.dat": "fake-text", "dir.txt": "fake-d2", "sub/x.txt": "fake-x2"})
	if got := diff(t, a, "bin.dat"); got.Reason != reasonBinary {
		t.Errorf("项目中是二进制文件时 Diff = %+v", got)
	}
	os.Remove(filepath.Join(project, "dir.txt"))
	os.Mkdir(filepath.Join(project, "dir.txt"), 0o755)
	if got := diff(t, a, "dir.txt"); got.Reason != reasonSpecial {
		t.Errorf("项目中是目录时 Diff = %+v", got)
	}
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"x.txt": "fake-secret"})
	os.RemoveAll(filepath.Join(project, "sub"))
	dirLink(t, outside, filepath.Join(project, "sub"))
	if got := diff(t, a, "sub/x.txt"); got.Reason != reasonSpecial || len(got.Lines) != 0 {
		t.Errorf("项目中的父目录是链接时 Diff = %+v", got)
	}
}

// 项目文件夹不存在时返回错误。
func TestDiffProjectGone(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2"})
	if err := os.RemoveAll(project); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Diff("a.txt"); err == nil {
		t.Fatal("项目文件夹不存在时 Diff 应该返回错误")
	}
}

// 逐行比较的计算量超过上限时，旧内容全部列为删除，新内容全部列为新增。
func TestDiffLinesTooMany(t *testing.T) {
	var old, cur []string
	for i := range 2100 {
		old = append(old, fmt.Sprintf("old %d", i))
		cur = append(cur, fmt.Sprintf("new %d", i))
	}
	got := diffLines(strings.Join(old, "\n"), strings.Join(cur, "\n"))
	if len(got) != 4200 || got[0] != (Line{Op: OpDel, Text: "old 0"}) || got[2100] != (Line{Op: OpAdd, Text: "new 0"}) {
		t.Fatalf("diffLines 返回 %d 行，开头 %+v", len(got), got[:1])
	}
}

// openRegular 和 readRegular 拒绝不存在的路径和目录。
func TestReadRegularRejects(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "d"), 0o755)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, _, err := readRegular(root, "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("readRegular(missing) = %v", err)
	}
	if _, _, err := readRegular(root, "d"); !errors.Is(err, errSpecial) {
		t.Errorf("readRegular(d) = %v，want errSpecial", err)
	}
}
