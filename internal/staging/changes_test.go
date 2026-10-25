package staging

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func changes(t *testing.T, a *Area) []Change {
	t.Helper()
	got, err := a.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	return got
}

func TestChangesEmpty(t *testing.T) {
	_, a := newArea(t)
	if got := changes(t, a); got == nil || len(got) != 0 {
		t.Fatalf("没有改动时 Changes = %#v，want 空切片", got)
	}
}

func TestChangesKinds(t *testing.T) {
	_, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2", "new/c.txt": "fake-c"})
	os.Remove(filepath.Join(a.Files(), "sub", "b.txt"))
	want := []Change{
		{Path: "a.txt", Kind: Modified},
		{Path: "new/c.txt", Kind: Added},
		{Path: "sub/b.txt", Kind: Deleted},
	}
	if got := changes(t, a); !reflect.DeepEqual(got, want) {
		t.Fatalf("Changes = %+v\nwant %+v", got, want)
	}
}

// 内容改回基线的文件不算改动。
func TestChangesIgnoresRestoredContent(t *testing.T) {
	_, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2"})
	stage(t, a, map[string]string{"a.txt": "fake-a"})
	if got := changes(t, a); len(got) != 0 {
		t.Fatalf("Changes = %+v", got)
	}
}

func TestChangesConflict(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2", "new.txt": "fake-new", "same.txt": "fake-same"})
	os.Remove(filepath.Join(a.Files(), "sub", "b.txt"))
	// 运行期间，项目中的同一批文件也被修改。same.txt 与暂存区内容相同，项目中已经存在，也算冲突。
	writeTree(t, project, map[string]string{
		"a.txt":     "fake-a-by-user",
		"new.txt":   "fake-new-by-user",
		"sub/b.txt": "fake-b-by-user",
		"same.txt":  "fake-same",
	})
	want := []Change{
		{Path: "a.txt", Kind: Modified, Conflict: true},
		{Path: "new.txt", Kind: Added, Conflict: true},
		{Path: "same.txt", Kind: Added, Conflict: true},
		{Path: "sub/b.txt", Kind: Deleted, Conflict: true},
	}
	if got := changes(t, a); !reflect.DeepEqual(got, want) {
		t.Fatalf("Changes = %+v\nwant %+v", got, want)
	}
}

// 暂存区新增的文件在项目中是同名目录时，也算冲突。
func TestChangesConflictWithProjectDir(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"x": "fake-file"})
	os.Mkdir(filepath.Join(project, "x"), 0o755)
	if got := changes(t, a); len(got) != 1 || !got[0].Conflict {
		t.Fatalf("Changes = %+v，want x 冲突", got)
	}
}

func TestChangesSpecial(t *testing.T) {
	_, a := newArea(t)
	outside := t.TempDir()
	// 原来的文件被换成目录或目录链接。
	os.Remove(filepath.Join(a.Files(), "a.txt"))
	os.Mkdir(filepath.Join(a.Files(), "a.txt"), 0o755)
	dirLink(t, outside, filepath.Join(a.Files(), "link"))
	want := []Change{
		{Path: "a.txt", Kind: Modified, Special: true},
		{Path: "link", Kind: Added, Special: true},
	}
	if got := changes(t, a); !reflect.DeepEqual(got, want) {
		t.Fatalf("Changes = %+v\nwant %+v", got, want)
	}
}

// 暂存区中的目录被换成指向别处的链接时，原来的文件按删除处理，不读取链接的目标。
func TestChangesDoesNotFollowDirLink(t *testing.T) {
	_, a := newArea(t)
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"b.txt": "fake-b", "evil.txt": "fake-evil"})
	os.RemoveAll(filepath.Join(a.Files(), "sub"))
	dirLink(t, outside, filepath.Join(a.Files(), "sub"))
	for _, c := range changes(t, a) {
		if c.Path == "sub/evil.txt" {
			t.Fatal("Changes 进入了指向暂存区以外的链接")
		}
		if c.Path == "sub/b.txt" && c.Kind != Modified && c.Kind != Deleted {
			t.Errorf("sub/b.txt：%+v", c)
		}
	}
}

func TestChangesSymlinkFile(t *testing.T) {
	_, a := newArea(t)
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"secret.txt": "fake-outside"})
	os.Remove(filepath.Join(a.Files(), "a.txt"))
	symlink(t, filepath.Join(outside, "secret.txt"), filepath.Join(a.Files(), "a.txt"))
	want := []Change{{Path: "a.txt", Kind: Modified, Special: true}}
	if got := changes(t, a); !reflect.DeepEqual(got, want) {
		t.Fatalf("Changes = %+v\nwant %+v", got, want)
	}
}

func TestChangesAfterOpen(t *testing.T) {
	project, a := newArea(t)
	stage(t, a, map[string]string{"a.txt": "fake-a2", "new.txt": "fake-new"})
	before := changes(t, a)
	b, err := Open(project, a.dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if after := changes(t, b); !reflect.DeepEqual(before, after) {
		t.Fatalf("Open 之后 Changes = %+v，之前 %+v", after, before)
	}
}

// shortRacyWindow 缩短 racyWindow，并等到暂存区中的文件都比它旧，这样 Changes 会缓存哈希。
func shortRacyWindow(t *testing.T) {
	t.Helper()
	old := racyWindow
	racyWindow = 50 * time.Millisecond
	t.Cleanup(func() { racyWindow = old })
	time.Sleep(100 * time.Millisecond)
}

// 缓存了哈希以后，内容改了但大小和修改时间不变的文件仍然列为改动（#76 AC-3）。
func TestChangesCacheSeesSameSizeEdit(t *testing.T) {
	_, a := newArea(t)
	shortRacyWindow(t)
	if got := changes(t, a); len(got) != 0 {
		t.Fatalf("Changes = %+v", got)
	}
	if (runtime.GOOS == "linux" || runtime.GOOS == "windows") && len(a.sums) == 0 {
		t.Fatal("Changes 没有缓存哈希")
	}
	p := filepath.Join(a.Files(), "a.txt")
	before, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("fake-A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("没有保留大小和修改时间：%v %v → %v %v", before.Size(), before.ModTime(), after.Size(), after.ModTime())
	}
	want := []Change{{Path: "a.txt", Kind: Modified}}
	if got := changes(t, a); !reflect.DeepEqual(got, want) {
		t.Fatalf("Changes = %+v\nwant %+v", got, want)
	}
}

// 没有新改动时，第二次 Changes 沿用哈希，耗时不到第一次的一半（#76 AC-2）。
func TestChangesCacheIsFaster(t *testing.T) {
	if testing.Short() {
		t.Skip("要创建 5000 个文件")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("这个系统不缓存哈希")
	}
	project := t.TempDir()
	files := map[string]string{"big.bin": strings.Repeat("fake-big", 4<<20)}
	for i := range 5000 {
		files[fmt.Sprintf("d%d/f%d.txt", i/500, i)] = "fake-small"
	}
	writeTree(t, project, files)
	a, err := Create(project, filepath.Join(t.TempDir(), "area"), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	shortRacyWindow(t)
	start := time.Now()
	if got := changes(t, a); len(got) != 0 {
		t.Fatalf("Changes = %d 个改动", len(got))
	}
	first := time.Since(start)
	start = time.Now()
	if got := changes(t, a); len(got) != 0 {
		t.Fatalf("Changes = %d 个改动", len(got))
	}
	second := time.Since(start)
	t.Logf("第一次 %v，第二次 %v", first, second)
	if second*2 >= first {
		t.Fatalf("第二次 %v 没有少于第一次 %v 的一半", second, first)
	}
}
