package staging

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
)

// writeTree 在 root 下按斜杠路径写入文件。
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// readTree 读出 root 下全部普通文件的内容，用于比较目录有没有变化。
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		got[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// symlink 创建符号链接。系统不允许创建时跳过测试，例如没有开发者模式的 Windows。
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("不能创建符号链接：%v", err)
	}
}

// dirLink 创建指向目录的链接。不能创建符号链接的 Windows 上改用 junction，它不需要特权。
func dirLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	}
	if runtime.GOOS != "windows" {
		t.Skip("不能创建符号链接")
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("不能创建 junction：%v %s", err, out)
	}
}

var baseProject = map[string]string{
	"a.txt":       "fake-a",
	"sub/b.txt":   "fake-b",
	".git/config": "fake-git-config",
	".env":        "FAKE_SECRET=fake-value",
}

// newArea 建立一个项目和它的暂存区。.env 被 skip 排除。
func newArea(t *testing.T) (project string, a *Area) {
	t.Helper()
	project = t.TempDir()
	writeTree(t, project, baseProject)
	a, err := Create(project, filepath.Join(t.TempDir(), "area"), func(rel string) bool { return rel == ".env" }, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return project, a
}

// stage 修改暂存区中的文件。
func stage(t *testing.T, a *Area, files map[string]string) {
	t.Helper()
	writeTree(t, a.Files(), files)
}

func TestCreateCopiesProject(t *testing.T) {
	project := t.TempDir()
	writeTree(t, project, baseProject)
	writeTree(t, project, map[string]string{"node_modules/x/index.js": "fake-dep"})
	os.Mkdir(filepath.Join(project, "empty"), 0o755)
	var skipped []string
	a, err := Create(project, filepath.Join(t.TempDir(), "area"), func(rel string) bool {
		if rel == ".env" || rel == "node_modules" {
			skipped = append(skipped, rel)
			return true
		}
		return false
	}, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := map[string]string{"a.txt": "fake-a", "sub/b.txt": "fake-b", ".git/config": "fake-git-config"}
	if got := readTree(t, a.Files()); !reflect.DeepEqual(got, want) {
		t.Errorf("暂存区内容 %v，want %v", got, want)
	}
	slices.Sort(skipped)
	if !slices.Equal(skipped, []string{".env", "node_modules"}) {
		t.Errorf("skip 收到的路径 %v", skipped)
	}
	if info, err := os.Stat(filepath.Join(a.Files(), "empty")); err != nil || !info.IsDir() {
		t.Error("空目录没有复制")
	}
}

func TestCreateWritesManifestOutsideFiles(t *testing.T) {
	_, a := newArea(t)
	data, err := os.ReadFile(filepath.Join(a.dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Base) != 3 {
		t.Errorf("基线清单有 %d 个文件，want 3：%v", len(m.Base), m.Base)
	}
	sum := sha256.Sum256([]byte("fake-b"))
	if m.Base["sub/b.txt"] != hex.EncodeToString(sum[:]) {
		t.Errorf("sub/b.txt 的 SHA-256 = %s", m.Base["sub/b.txt"])
	}
	if _, err := os.Stat(filepath.Join(a.Files(), "manifest.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("manifest.json 在挂载进沙箱的目录中")
	}
}

func TestCreateProgress(t *testing.T) {
	for _, n := range []int{0, 1, 250} {
		project := t.TempDir()
		files := map[string]string{}
		for i := range n {
			files[filepath.ToSlash(filepath.Join("d", string(rune('a'+i%26)), hex.EncodeToString([]byte{byte(i)})))] = "x"
		}
		writeTree(t, project, files)
		var calls [][2]int
		_, err := Create(project, filepath.Join(t.TempDir(), "area"), nil, nil, func(done, total int) {
			calls = append(calls, [2]int{done, total})
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) == 0 || calls[len(calls)-1] != [2]int{n, n} {
			t.Errorf("%d 个文件：progress 调用 %v，最后一次 want [%d %d]", n, calls, n, n)
		}
	}
	// progress 为 nil 时不报错。
	if _, err := Create(t.TempDir(), filepath.Join(t.TempDir(), "area"), nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}

// 项目中的符号链接可能指向项目以外，不能复制进沙箱。
func TestCreateSkipsSymlink(t *testing.T) {
	project, outside := t.TempDir(), t.TempDir()
	writeTree(t, outside, map[string]string{"secret.txt": "fake-outside"})
	writeTree(t, project, map[string]string{"a.txt": "fake-a"})
	dirLink(t, outside, filepath.Join(project, "linkdir"))
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(project, "link.txt")); err != nil {
		t.Logf("不能创建文件的符号链接，只检查目录链接：%v", err)
	}
	a, err := Create(project, filepath.Join(t.TempDir(), "area"), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := readTree(t, a.Files()); !reflect.DeepEqual(got, map[string]string{"a.txt": "fake-a"}) {
		t.Errorf("暂存区内容 %v", got)
	}
}

func TestCreateRejectsExistingArea(t *testing.T) {
	project, a := newArea(t)
	if _, err := Create(project, a.dir, nil, nil, nil); err == nil {
		t.Fatal("在已有的暂存区上 Create 没有报错")
	}
	if got := readTree(t, a.Files()); len(got) != 3 {
		t.Errorf("已有的暂存区被改动：%v", got)
	}
}

func TestCreateCleansUpOnError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "area")
	if _, err := Create(filepath.Join(t.TempDir(), "fake-missing"), dir, nil, nil, nil); err == nil {
		t.Fatal("项目不存在时 Create 没有报错")
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Error("Create 失败后留下了暂存区")
	}
}

func TestOpen(t *testing.T) {
	project, a := newArea(t)
	b, err := Open(project, a.dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if b.Files() != a.Files() || !reflect.DeepEqual(b.base, a.base) {
		t.Error("Open 读回的暂存区与 Create 不同")
	}
	if _, err := Open(project, t.TempDir(), nil); err == nil {
		t.Error("Open 打开了没有基线清单的目录")
	}
}

func TestDiscard(t *testing.T) {
	project, a := newArea(t)
	if err := a.Discard(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.dir); !errors.Is(err, fs.ErrNotExist) {
		t.Error("Discard 后暂存区仍然存在")
	}
	if got := readTree(t, project); !reflect.DeepEqual(got, baseProject) {
		t.Errorf("Discard 改动了项目：%v", got)
	}
}
