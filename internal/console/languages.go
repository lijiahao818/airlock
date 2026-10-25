package console

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// 本文件检测项目使用的编程语言，用来在新建工作舱时提示语言环境不匹配（issue #97）。
// 取值与 preset.json 的 languages 相同。只看文件名，不读文件内容。

// languageFiles 把项目的标志性文件映射到语言。名称比较不区分大小写。
var languageFiles = map[string]string{
	"package.json":     "node",
	"go.mod":           "go",
	"cargo.toml":       "rust",
	"requirements.txt": "python",
	"pyproject.toml":   "python",
	"pom.xml":          "java",
	"build.gradle":     "java",
	"build.gradle.kts": "java",
}

// languageExts 把源文件的扩展名映射到语言。
var languageExts = map[string]string{
	".py": "python",
	".js": "node", ".mjs": "node", ".cjs": "node", ".mts": "node", ".cts": "node",
	".ts": "node", ".tsx": "node", ".jsx": "node",
	".go":   "go",
	".rs":   "rust",
	".java": "java",
	".c":    "c",
	".cpp":  "cpp", ".cc": "cpp", ".cxx": "cpp", ".hpp": "cpp",
}

// skippedDirs 是 ADR-008 的依赖目录，加上 .git 和常见的构建产物目录。
// 目录下的文件不代表项目本身，而且数量多，跳过后检测更快。
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true, "__pycache__": true,
	".pytest_cache": true, ".mypy_cache": true, "target": true, ".gradle": true,
	".next": true, ".nuxt": true, "dist": true, "build": true,
}

// detectLimit 是最多查看的文件数。超过上限时按已经看到的部分判定，检测不能让新建页面卡住。
const detectLimit = 4096

var errNotADir = errors.New("找不到这个文件夹。请重新选择文件夹。")

// detectLanguages 返回 dir 中的项目文件表明的语言，按名称排序。dir 必须是存在的文件夹，
// 否则返回 errNotADir。单个子目录读不了时跳过，不影响结果。
func detectLanguages(dir string) ([]string, error) {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, errNotADir
	}
	found := map[string]bool{}
	seen := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != dir && skippedDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if seen++; seen > detectLimit {
			return fs.SkipAll
		}
		name := strings.ToLower(d.Name())
		if l, ok := languageFiles[name]; ok {
			found[l] = true
		} else if l, ok := languageExts[strings.ToLower(filepath.Ext(name))]; ok {
			found[l] = true
		}
		return nil
	})
	if len(found) == 0 {
		return []string{}, nil
	}
	return slices.Sorted(maps.Keys(found)), nil
}
