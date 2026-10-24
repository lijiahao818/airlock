package gate

import (
	"crypto/subtle"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// route 把 /<供应商>/<剩余路径> 拆开。路径不规范时返回 false，防止 .. 越出上游的路径前缀。
func route(p string) (name, rest string, ok bool) {
	if p == "" || path.Clean(p) != p {
		return "", "", false
	}
	name, rest, _ = strings.Cut(strings.TrimPrefix(p, "/"), "/")
	if _, ok := providers[name]; !ok {
		return "", "", false
	}
	if rest != "" {
		rest = "/" + rest
	}
	return name, rest, true
}

// serveRoute 校验占位符，换上真实密钥，把请求转发到供应商的接口。
func (g *Gate) serveRoute(w http.ResponseWriter, r *http.Request, name, rest string) {
	p := providers[name]
	target, err := url.Parse(p.upstream)
	if err != nil {
		http.Error(w, "Airlock 闸门的路由表有误", http.StatusInternalServerError)
		return
	}
	port, _ := strconv.Atoi(target.Port())
	if port == 0 {
		port = 443
	}
	e := Event{Host: target.Hostname(), Port: port, Method: r.Method, Path: r.URL.Path}

	got := r.Header.Get(p.header)
	if p.bearer {
		got = strings.TrimPrefix(got, "Bearer ")
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(g.placeholders[name])) != 1 {
		g.deny(w, e, "bad-placeholder", http.StatusForbidden)
		return
	}
	secret, err := g.cfg.Secret(name)
	if err != nil || secret == "" {
		g.deny(w, e, "route", http.StatusBadGateway)
		return
	}
	e.Rule, e.Allowed, e.Provider = "route", true, name
	g.emit(e)

	value := secret
	if p.bearer {
		value = "Bearer " + secret
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.URL.Path = target.Path + rest
			pr.Out.URL.RawPath = ""
			pr.Out.Host = ""
			// 只留下闸门换上的密钥，沙箱带来的其他凭据一律去掉。
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("X-Api-Key")
			pr.Out.Header.Del("Proxy-Authorization")
			pr.Out.Header.Set(p.header, value)
		},
		// 默认的错误处理会写日志。这里只返回固定文字，不输出请求的任何内容。
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "Airlock 闸门连不上模型接口", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}
