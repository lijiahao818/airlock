package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedFilesAreCurrent 是漂移锁：契约代码改了却没有重新生成，测试失败。
func TestGeneratedFilesAreCurrent(t *testing.T) {
	want, err := generate("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("../..", outDir)
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != content {
			t.Errorf("%s 不是最新的。运行 go run ./tools/tsgen 重新生成。", name)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if _, ok := want[e.Name()]; !ok {
			t.Errorf("%s 不是生成器的产物。删除它，或把它的来源加入 packages。", e.Name())
		}
	}
}

func renderSource(t *testing.T, src string) (string, error) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "p", "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := parsePackage(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	return render(p, map[string]*pkgInfo{p.name: p})
}

func TestRenderTypes(t *testing.T) {
	got, err := renderSource(t, `package p

type Kind string

const (
	A Kind = "a"
	B Kind = "b"
)

type Free string

type Inner struct {
	X int `+"`json:\"x\"`"+`
}

type Outer struct {
	Inner
	Ptr    *Inner            `+"`json:\"ptr\"`"+`
	Opt    *Inner            `+"`json:\"opt,omitempty\"`"+`
	Raw    []byte            `+"`json:\"raw\"`"+`
	Ptrs   []*Inner          `+"`json:\"ptrs\"`"+`
	Labels map[string]Kind   `+"`json:\"labels\"`"+`
	Skip   string            `+"`json:\"-\"`"+`
	hidden string
}

type NoTags struct {
	Y int
}
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`export type Kind = "a" | "b";`,
		`export type Free = string;`,
		"  x: number;\n  ptr: Inner | null;",
		"  opt?: Inner;",
		"  raw: string;",
		"  ptrs: (Inner | null)[];",
		"  labels: Record<string, Kind>;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("输出缺少 %q：\n%s", want, got)
		}
	}
	for _, bad := range []string{"Skip", "hidden", "NoTags"} {
		if strings.Contains(got, bad) {
			t.Errorf("输出不应包含 %s：\n%s", bad, got)
		}
	}
}

func TestRenderRejectsUnsupported(t *testing.T) {
	for name, field := range map[string]string{
		"chan":      "C chan int `json:\"c\"`",
		"非字符串键":     "M map[int]string `json:\"m\"`",
		"没有生成的类型":   "N NoTags `json:\"n\"`",
		"interface": "I interface{} `json:\"i\"`",
		"未知包":       "T other.Type `json:\"t\"`",
	} {
		src := "package p\n\ntype NoTags struct{ Y int }\n\ntype S struct {\n\t" + field + "\n}\n"
		if _, err := renderSource(t, src); err == nil {
			t.Errorf("%s：应当报错", name)
		}
	}
}
