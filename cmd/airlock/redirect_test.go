package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRedirectedNotInPackage(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "Airlock")
	// 有其他应用的私有目录，但其中没有本次写入的文件。
	if err := os.MkdirAll(filepath.Join(base, "Packages", "Fake_pkg", "LocalCache", "Local", "Airlock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if r, err := redirected(base, dir); err != nil || r {
		t.Fatalf("redirected = %v, %v", r, err)
	}
	// 检查用的文件已经删除。
	if ents, err := os.ReadDir(dir); err != nil || len(ents) != 0 {
		t.Fatalf("数据目录中留下了 %v, %v", ents, err)
	}
}

func TestInPackage(t *testing.T) {
	base := t.TempDir()
	rel := filepath.Join("Airlock", ".redirect-check-1")
	if r, err := inPackage(base, rel); err != nil || r {
		t.Fatalf("没有 Packages 目录时 inPackage = %v, %v", r, err)
	}
	// 模拟 Windows 把写入的文件转存到打包应用的私有目录。
	p := filepath.Join(base, "Packages", "Fake_pkg", "LocalCache", "Local", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if r, err := inPackage(base, rel); err != nil || !r {
		t.Fatalf("文件被转存时 inPackage = %v, %v", r, err)
	}
}

func TestRedirectedUnwritable(t *testing.T) {
	base := t.TempDir()
	// 数据目录的位置被一个普通文件占用，不能写入。
	dir := filepath.Join(base, "Airlock")
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := redirected(base, dir); err == nil {
		t.Fatal("数据目录不能写入时 redirected 没有返回错误")
	}
}
