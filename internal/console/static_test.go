package console

import (
	"net/http"
	"strings"
	"testing"
)

// AC-9 的另一半：没有构建前端时，go:embed 用仓库中的占位页面，构建仍然成功。
// 静态页面也做 Host 检查（AC-3），但不要求令牌。
func TestStatic(t *testing.T) {
	e := newEnv(t)
	resp := e.request(t, http.MethodGet, "/", "", "", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q，want text/html", ct)
	}
	if got := body(t, resp); !strings.Contains(got, "还没有构建") {
		t.Errorf("响应中没有占位文案：%q", got)
	}

	// 静态页面同样做 Host 检查。
	resp = e.request(t, http.MethodGet, "/", "", "", "evil.com", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("静态页面没有做 Host 检查：状态码 = %d，want %d", resp.StatusCode, http.StatusForbidden)
	}
	body(t, resp)

	// 不存在的文件回退到首页，由前端路由处理，不返回英文 404。
	resp = e.request(t, http.MethodGet, "/no-such-page", "", "", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("缺失的路径：状态码 = %d，want %d", resp.StatusCode, http.StatusOK)
	}
	if got := body(t, resp); !strings.Contains(got, "还没有构建") {
		t.Errorf("缺失的路径没有回退到首页：%q", got)
	}

	// 直接请求首页文件也一样。
	resp = e.request(t, http.MethodGet, "/index.html", "", "", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("首页文件：状态码 = %d，want %d", resp.StatusCode, http.StatusOK)
	}
	body(t, resp)
}
