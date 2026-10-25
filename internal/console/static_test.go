package console

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, fsys fs.FS, target string) (int, string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	serveDist(fsys, rec, httptest.NewRequest(http.MethodGet, target, nil))
	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, string(body), resp.Header.Get("Content-Type")
}

// #83 AC-2：没有构建前端时，打开控制台显示中文提示页，说明先构建前端。
// 提示页放在代码里，不依赖 dist 中的文件，所以本地构建过前端也不影响这条测试。
func TestStaticNotBuilt(t *testing.T) {
	fsys := fstest.MapFS{".gitkeep": {}}
	for _, target := range []string{"/", "/no-such-page", "/index.html"} {
		code, body, ct := get(t, fsys, target)
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d，want %d", target, code, http.StatusOK)
		}
		if !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s 的 Content-Type = %q，want text/html", target, ct)
		}
		if !strings.Contains(body, "还没有构建") || !strings.Contains(body, "npm run build") {
			t.Errorf("GET %s 没有说明怎么构建：%q", target, body)
		}
	}
}

func TestStaticServesBuiltFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>Airlock</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
		".gitkeep":      {},
	}
	if code, body, _ := get(t, fsys, "/"); code != http.StatusOK || !strings.Contains(body, "Airlock") {
		t.Fatalf("GET / = %d %q", code, body)
	}
	if code, body, _ := get(t, fsys, "/assets/app.js"); code != http.StatusOK || body != "console.log(1)" {
		t.Fatalf("GET /assets/app.js = %d %q", code, body)
	}
	// 缺失的路径和目录回退到首页，由前端路由处理，不返回英文 404，也不列出目录中的文件。
	for _, target := range []string{"/no-such-page", "/assets/"} {
		if code, body, _ := get(t, fsys, target); code != http.StatusOK || !strings.Contains(body, "Airlock") {
			t.Errorf("GET %s = %d %q，应该回退到首页", target, code, body)
		}
	}
}

// 静态页面做 Host 检查（T-12），但不要求令牌。走完整的服务器，
// 断言只看状态码，不依赖本地是否构建过前端。
func TestStaticHostCheck(t *testing.T) {
	e := newEnv(t)
	resp := e.request(t, http.MethodGet, "/", "", "", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusOK)
	}
	body(t, resp)

	resp = e.request(t, http.MethodGet, "/", "", "", "evil.com", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("静态页面没有做 Host 检查：状态码 = %d，want %d", resp.StatusCode, http.StatusForbidden)
	}
	body(t, resp)
}
