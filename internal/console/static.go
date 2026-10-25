package console

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// dist 是前端的构建结果。仓库只放一个占位文件 .gitkeep，前端构建（见 docs/architecture.md
// 第 5.10 节）后输出到这里。
var dist, _ = fs.Sub(distFS, "dist")

// notBuiltPage 是没有构建前端时的提示页面。构建步骤见 README.md。
const notBuiltPage = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Airlock</title>
</head>
<body>
<p>Airlock 控制台的前端还没有构建。请在 web 目录中运行 npm ci 和 npm run build，再重新启动 Airlock。</p>
</body>
</html>
`

// serveStatic 提供前端的构建结果。
func serveStatic(w http.ResponseWriter, r *http.Request) { serveDist(dist, w, r) }

// serveDist 提供 fsys 中的前端文件。没有 index.html 时说明前端没有构建，
// 任何路径都返回中文提示页。目录和缺失的路径回退到首页，由前端路由处理，不提供目录列表。
func serveDist(fsys fs.FS, w http.ResponseWriter, r *http.Request) {
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, notBuiltPage)
		return
	}
	if p := strings.TrimPrefix(r.URL.Path, "/"); p != "" {
		if st, err := fs.Stat(fsys, p); err != nil || st.IsDir() {
			r.URL.Path = "/"
		}
	}
	http.FileServerFS(fsys).ServeHTTP(w, r)
}
