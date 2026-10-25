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
		"嵌入未知包":     "other.Type",
		"嵌入指针":      "*NoTags",
		"嵌入不存在的类型":  "Missing",
		"嵌入非结构体":    "Kind",
		"嵌入的字段不支持":  "Bad",
	} {
		// Z 让 S 带上 json 标签，S 才会被生成。Bad 没有标签，不单独生成，只在被嵌入时报错。
		src := "package p\n\ntype NoTags struct{ Y int }\n\ntype Kind string\n\ntype Bad struct{ C chan int }\n\ntype S struct {\n\t" + field + "\n\tZ int `json:\"z\"`\n}\n"
		if _, err := renderSource(t, src); err == nil {
			t.Errorf("%s：应当报错", name)
		}
	}
}

// 标签只写选项时用 Go 字段名作键。带标签的嵌入字段按普通字段输出，键是类型名。
func TestRenderTagFallbacks(t *testing.T) {
	got, err := renderSource(t, "package p\n\ntype Inner struct {\n\tX int `json:\"x\"`\n}\n\ntype S struct {\n\tName string `json:\",omitempty\"`\n\tInner `json:\",omitempty\"`\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"  Name?: string;", "  Inner?: Inner;"} {
		if !strings.Contains(got, want) {
			t.Errorf("输出缺少 %q：\n%s", want, got)
		}
	}
}

func TestParsePackageErrors(t *testing.T) {
	root := t.TempDir()
	for dir, src := range map[string]string{"empty": "", "broken": "package p\n\ntype S struct {"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if src != "" {
			if err := os.WriteFile(filepath.Join(root, dir, "p.go"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := parsePackage(root, "empty"); err == nil || !strings.Contains(err.Error(), "没有 Go 文件") {
		t.Errorf("空目录：期望报没有 Go 文件，得到 %v", err)
	}
	if _, err := parsePackage(root, "broken"); err == nil {
		t.Error("语法错误：应当报错")
	}
	if _, err := parsePackage(root, "["); err == nil {
		t.Error("非法的路径模式：应当报错")
	}
}

func TestGenerateErrors(t *testing.T) {
	saved := packages
	t.Cleanup(func() { packages = saved })
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "p", "p.go"), []byte("package p\n\ntype S struct {\n\tC chan int `json:\"c\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	packages = []string{"p"}
	if _, err := generate(root); err == nil || !strings.Contains(err.Error(), "p：") {
		t.Errorf("类型不支持时应报出包路径，得到 %v", err)
	}
	packages = []string{"missing"}
	if _, err := generate(root); err == nil {
		t.Error("包不存在时应当报错")
	}
}

// run 把生成结果写入 root 下的 outDir。生成失败、目录建不了、文件写不进去时返回 1。
func TestRun(t *testing.T) {
	saved := packages
	t.Cleanup(func() { packages = saved })
	packages = []string{"p"}
	newRoot := func(t *testing.T) string {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "p"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "p", "p.go"), []byte("package p\n\ntype S struct {\n\tX int `json:\"x\"`\n}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}

	t.Run("成功", func(t *testing.T) {
		root := newRoot(t)
		var stderr strings.Builder
		if code := run(root, &stderr); code != 0 {
			t.Fatalf("code = %d，stderr = %q", code, stderr.String())
		}
		b, err := os.ReadFile(filepath.Join(root, outDir, "p.ts"))
		if err != nil || !strings.Contains(string(b), "x: number;") {
			t.Fatalf("p.ts = %q, %v", b, err)
		}
	})
	t.Run("生成失败", func(t *testing.T) {
		var stderr strings.Builder
		if code := run(t.TempDir(), &stderr); code != 1 || !strings.Contains(stderr.String(), "tsgen：") {
			t.Fatalf("code = %d，stderr = %q", code, stderr.String())
		}
	})
	t.Run("输出目录建不了", func(t *testing.T) {
		root := newRoot(t)
		if err := os.WriteFile(filepath.Join(root, "web"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		var stderr strings.Builder
		if code := run(root, &stderr); code != 1 {
			t.Fatalf("code = %d，want 1", code)
		}
	})
	t.Run("文件写不进去", func(t *testing.T) {
		root := newRoot(t)
		if err := os.MkdirAll(filepath.Join(root, outDir, "p.ts"), 0o755); err != nil {
			t.Fatal(err)
		}
		var stderr strings.Builder
		if code := run(root, &stderr); code != 1 {
			t.Fatalf("code = %d，want 1", code)
		}
	})
}
