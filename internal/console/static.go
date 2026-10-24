package console

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// dist 是前端的构建结果。仓库自带的 index.html 是占位页面，
// 前端构建（见 docs/architecture.md 第 5.10 节）后覆盖整个目录。
var dist, _ = fs.Sub(distFS, "dist")

// files 提供前端文件。
var files = http.FileServerFS(dist)
