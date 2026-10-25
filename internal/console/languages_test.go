package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 检测只看文件名：扩展名映射到语言，标志性文件（package.json 等）也算。
func TestDetectLanguages(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"calc.py", "pkg/main.py",
		"package.json", "src/app.ts",
		"go.mod", "main.go",
		"Cargo.toml", "src/lib.rs",
		"App.java", "engine.c", "util.cpp",
		"notes.txt", "logo.png",
	}
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := detectLanguages(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(got, " "); got != "c cpp go java node python rust" {
		t.Fatalf("detectLanguages = %q, want %q", got, "c cpp go java node python rust")
	}
}

// 依赖目录和 .git 中的文件不代表项目本身，不参与检测。
func TestDetectLanguagesSkipsDeps(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"node_modules/left-pad/index.js", ".git/hooks/go.so", "dist/bundle.js", "main.py"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := detectLanguages(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "python" {
		t.Fatalf("detectLanguages = %v, want [python]", got)
	}
}

// 没有源文件的文件夹返回空列表，不返回错误。
func TestDetectLanguagesEmpty(t *testing.T) {
	got, err := detectLanguages(t.Context(), t.TempDir())
	if err != nil || strings.Join(got, ",") != "" {
		t.Fatalf("detectLanguages = %v, %v, want [], nil", got, err)
	}
	if _, err := detectLanguages(t.Context(), filepath.Join(t.TempDir(), "no-such-dir")); err == nil {
		t.Fatal("文件夹不存在时应该返回错误")
	}
}

// 请求取消时停止遍历，立即返回。
func TestDetectLanguagesCancel(t *testing.T) {
	dir := t.TempDir()
	for i := range 20 {
		sub := filepath.Join(dir, fmt.Sprintf("d%02d", i))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "calc.py"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := detectLanguages(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 1 {
		t.Fatalf("已取消的检测不应该遍历全部目录：got %v", got)
	}
}

// project-languages 接口返回检测出的语言。路径不是完整路径时给出中文提示。
func TestProjectLanguagesAPI(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "calc.py"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	resp := e.request(t, http.MethodPost, "/api/project-languages",
		`{"path":`+quoteJSON(dir)+`}`, fakeToken, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusOK)
	}
	if got := body(t, resp); !strings.Contains(got, `"languages":["python"]`) {
		t.Fatalf("响应 = %q，want languages 含 python", got)
	}

	resp = e.request(t, http.MethodPost, "/api/project-languages", `{"path":"relative/path"}`, fakeToken, "", "")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body(t, resp), "完整路径") {
		t.Fatalf("相对路径：状态码 = %d，正文 = %q", resp.StatusCode, body(t, resp))
	}

	resp = e.request(t, http.MethodPost, "/api/project-languages",
		`{"path":`+quoteJSON(filepath.Join(dir, "no-such"))+`}`, fakeToken, "", "")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body(t, resp), "重新选择文件夹") {
		t.Fatalf("不存在的文件夹：状态码 = %d，正文 = %q", resp.StatusCode, body(t, resp))
	}
}

// quoteJSON 把路径包成 JSON 字符串，处理 Windows 路径中的反斜杠。
func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
