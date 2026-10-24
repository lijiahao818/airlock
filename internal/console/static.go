package console

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// dist 是前端的构建结果。仓库自带的 index.html 是占位页面，
// 前端构建（见 docs/architecture.md 第 5.10 节）后覆盖整个目录。
var dist, _ = fs.Sub(distFS, "dist")

// files 提供前端文件。
var files = http.FileServerFS(dist)

// serveStatic 提供前端文件。目录和缺失的路径回退到首页，由前端路由处理，
// 不提供目录列表。
func serveStatic(w http.ResponseWriter, r *http.Request) {
	if p := strings.TrimPrefix(r.URL.Path, "/"); p != "" {
		if st, err := fs.Stat(dist, p); err != nil || st.IsDir() {
			r.URL.Path = "/"
		}
	}
	files.ServeHTTP(w, r)
}
