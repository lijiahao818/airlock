package staging

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// rawPath 让 Windows 保留文件名末尾的点和空格。Docker Desktop 把沙箱中的这类文件原样写入暂存区。
func rawPath(p string) string {
	if runtime.GOOS == "windows" {
		return `\\?\` + p
	}
	return p
}

// 文件名以点或空格结尾时，Windows 读写的是去掉末尾字符后的另一个文件。
// 这类文件必须列出并标为不能写回，也不能借它们读写项目中的同名文件。
func TestBadNameIsListedButNotApplied(t *testing.T) {
	project := t.TempDir()
	writeTree(t, project, map[string]string{"trail": "fake-project-trail"})
	a, err := Create(project, filepath.Join(t.TempDir(), "area"), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"trail.", "space ", "dir./inner.txt"} {
		p := filepath.Join(a.Files(), filepath.FromSlash(name))
		// MkdirAll 不能处理 \\?\ 前缀，所以只建最后一级目录。
		if err := os.Mkdir(rawPath(filepath.Dir(p)), 0o755); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
		if err := os.WriteFile(rawPath(p), []byte("fake-agent"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]Change{}
	for _, c := range changes(t, a) {
		got[c.Path] = c
	}
	for _, name := range []string{"trail.", "space ", "dir."} {
		if c := got[name]; c.Kind != Added || !c.Special || c.Applied {
			t.Errorf("%q：%+v", name, c)
		}
		if err := a.Apply(name); !errors.Is(err, errBadName) {
			t.Errorf("Apply(%q) = %v，want errBadName", name, err)
		}
		if err := a.Revert(name); !errors.Is(err, errBadName) {
			t.Errorf("Revert(%q) = %v，want errBadName", name, err)
		}
	}
	if len(got) != 3 {
		t.Errorf("Changes = %+v", got)
	}
	before := readTree(t, project)
	if before["trail"] != "fake-project-trail" {
		t.Fatalf("项目中的 trail 被修改：%q", before["trail"])
	}
	for _, name := range []string{"space", "dir"} {
		if _, err := os.Lstat(filepath.Join(project, name)); !os.IsNotExist(err) {
			t.Errorf("项目中出现了 %s：%v", name, err)
		}
	}
	// 完成审查时，暂存区连同这类文件一起删除。
	if err := a.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
}
