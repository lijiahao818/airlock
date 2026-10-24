package webterm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body)
}

// AC-7：没有构建前端时，首页说明怎么构建。
func TestPageNotBuilt(t *testing.T) {
	code, body := get(t, page(fstest.MapFS{".gitkeep": {}}), "/")
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "npm run build") {
		t.Fatalf("GET / = %d %q", code, body)
	}
	// 内嵌的 dist 在没有构建时也能提供首页。
	if code, _ := get(t, New(&fakeAttacher{}, "http://127.0.0.1:1"), "/"); code != http.StatusOK && code != http.StatusServiceUnavailable {
		t.Fatalf("内嵌页面 GET / = %d", code)
	}
}

func TestPageServesFilesWithoutListing(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":     {Data: []byte("<!doctype html><title>网页终端</title>")},
		"assets/term.js": {Data: []byte("console.log(1)")},
	}
	h := page(fsys)
	if code, body := get(t, h, "/"); code != http.StatusOK || !strings.Contains(body, "网页终端") {
		t.Fatalf("GET / = %d %q", code, body)
	}
	if code, body := get(t, h, "/assets/term.js"); code != http.StatusOK || body != "console.log(1)" {
		t.Fatalf("GET /assets/term.js = %d %q", code, body)
	}
	for _, path := range []string{"/assets/", "/missing.js", "/../go.mod"} {
		if code, _ := get(t, h, path); code != http.StatusNotFound {
			t.Errorf("GET %s = %d，应该是 404", path, code)
		}
	}
}
