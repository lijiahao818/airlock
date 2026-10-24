package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedirectedNotInPackage(t *testing.T) {
	base := t.TempDir()
	// 有其他应用的私有目录，但其中没有本次写入的文件。
	if err := os.MkdirAll(filepath.Join(base, "Packages", "Fake_pkg", "LocalCache", "Local", "Airlock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if r, err := redirected(base, "Airlock"); err != nil || r {
		t.Fatalf("redirected = %v, %v", r, err)
	}
	// 检查用的文件已经删除。
	if ents, err := os.ReadDir(filepath.Join(base, "Airlock")); err != nil || len(ents) != 0 {
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

func TestInPackageUnreadable(t *testing.T) {
	// 路径中有 NUL，不能列出 Packages。Windows 上对普通文件 ReadDir 的错误算作"不存在"，所以不用普通文件模拟。
	if _, err := inPackage("fake\x00base", "Airlock"); err == nil {
		t.Fatal("Packages 不能读取时 inPackage 没有返回错误")
	}
}

func TestRedirectedUnwritable(t *testing.T) {
	base := t.TempDir()
	// 数据目录的位置被一个普通文件占用，不能写入。
	if err := os.WriteFile(filepath.Join(base, "Airlock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := redirected(base, "Airlock"); err == nil {
		t.Fatal("数据目录不能写入时 redirected 没有返回错误")
	}
}

func TestBlocked(t *testing.T) {
	tests := []struct {
		name    string
		r       bool
		err     error
		blocked bool
		want    string // stderr 中必须有的下一步操作
	}{
		{"可以使用", false, nil, false, ""},
		{"被转存", true, nil, true, "在资源管理器中双击 airlock.exe"},
		{"不能写入", false, errors.New("fake: 拒绝访问"), true, "请检查这个目录的权限"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := blocked(&stderr, `C:\fake\Airlock`, tt.r, tt.err); got != tt.blocked {
				t.Fatalf("blocked = %v, want %v", got, tt.blocked)
			}
			if !strings.Contains(stderr.String(), tt.want) || (tt.want == "") != (stderr.Len() == 0) {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}
