// Command tsgen 从 Go 契约代码生成前端使用的 TypeScript 类型。
// 每个 Go 包生成一个文件，写入 web/src/generated/。
// 只导出带 json 标签的结构体和字符串枚举。见 docs/architecture.md 第 5 节。
package main

import (
	"fmt"
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
	files, err := generate(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tsgen：", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "tsgen：", err)
		os.Exit(1)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "tsgen：", err)
			os.Exit(1)
		}
	}
}
