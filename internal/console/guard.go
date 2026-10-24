package console

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

// ServeHTTP 先做安全检查，再把请求交给路由。检查对应 docs/security.md 的 T-12：
// Host 头防止 DNS 重绑定，Origin 头防止其他网页发起请求，令牌防止本机其他进程直接调用。
// 顺序是 Host、Origin、令牌。
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 控制台页面和接口都不允许被别的网页嵌入（防点击劫持），也不允许被嗅探内容类型。
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !localHost(r.Host) {
		http.Error(w, "Airlock 只能从本机地址打开。请用启动时给出的网址。", http.StatusForbidden)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
		http.Error(w, "Airlock 不接受其他页面发来的请求。请回到 Airlock 的页面操作。", http.StatusForbidden)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") &&
		(s.token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Airlock-Token")), []byte(s.token)) != 1) {
		http.Error(w, "控制台令牌缺失或不正确。请从 Airlock 启动时打开的页面进入。", http.StatusUnauthorized)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.mux.ServeHTTP(w, r)
		return
	}
	files.ServeHTTP(w, r)
}

// localHost 报告 Host 头是不是 127.0.0.1:<端口> 或 localhost:<端口>。
// 第 5.10 节要求带端口。控制台不在 80 端口上，正常的浏览器请求总会带端口。
func localHost(h string) bool {
	name, port, err := net.SplitHostPort(h)
	return err == nil && port != "" &&
		(strings.EqualFold(name, "127.0.0.1") || strings.EqualFold(name, "localhost"))
}
