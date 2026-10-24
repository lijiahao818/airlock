package webterm

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist 是 web/term 的构建结果。没有构建前端时只有 .gitkeep，go build 仍然成功。
//
//go:embed all:dist
var dist embed.FS

// page 提供网页终端的静态文件。没有构建前端时，首页告诉用户怎么构建。
func page(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			if _, err := fs.Stat(fsys, "index.html"); err != nil {
				http.Error(w, "网页终端的页面还没有构建。请在 web/term 目录中运行 npm ci 和 npm run build，再重新启动 Airlock。", http.StatusServiceUnavailable)
				return
			}
		} else if strings.HasSuffix(r.URL.Path, "/") {
			// 不列出目录中的文件。
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}
