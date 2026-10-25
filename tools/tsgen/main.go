// Command tsgen 从 Go 契约代码生成前端使用的 TypeScript 类型。
// 每个 Go 包生成一个文件，写入 web/src/generated/。
// 只导出带 json 标签的结构体和字符串枚举。见 docs/architecture.md 第 5 节。
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// packages 是要生成类型的 Go 包，路径相对于仓库根目录。
var packages = []string{
	"internal/gate",
	"internal/staging",
	"internal/audit",
	"internal/cabin",
	"internal/advisor",
	"internal/console",
	"presets",
}

const outDir = "web/src/generated"

func main() {
	os.Exit(run(".", os.Stderr))
}

// run 生成 root 下的契约代码对应的 TS 文件，写入 root 下的 outDir，返回退出码。
func run(root string, stderr io.Writer) int {
	files, err := generate(root)
	if err != nil {
		fmt.Fprintln(stderr, "tsgen：", err)
		return 1
	}
	dir := filepath.Join(root, outDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(stderr, "tsgen：", err)
		return 1
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			fmt.Fprintln(stderr, "tsgen：", err)
			return 1
		}
	}
	return 0
}
