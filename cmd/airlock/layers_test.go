package main

import (
	"go/build"
	"slices"
	"strings"
	"testing"
)

const module = "github.com/lijiahao818/airlock/"

var (
	base      = []string{"internal/gate", "internal/sandbox", "internal/staging", "internal/vault", "internal/desktop", "presets"}
	core      = append([]string{"internal/audit", "internal/cabin"}, base...)
	extension = []string{"internal/advisor", "internal/filecheck"}
)

// allowed 返回 pkg 可以导入的项目内包，规则见 docs/architecture.md 第 3.2 节和第 4.2 节。
func allowed(pkg string) []string {
	switch {
	case slices.Contains(base, pkg):
		return nil
	case pkg == "internal/audit":
		return []string{"internal/gate"}
	case pkg == "internal/cabin":
		return slices.DeleteFunc(slices.Clone(core), func(p string) bool { return p == "internal/cabin" || p == "internal/vault" })
	case slices.Contains(extension, pkg):
		return core
	case pkg == "internal/console", pkg == "internal/webterm":
		return append(slices.Clone(core), extension...)
	}
	return nil
}

func TestLayerImports(t *testing.T) {
	for _, pkg := range append(append(slices.Clone(core), extension...), "internal/console", "internal/webterm") {
		p, err := build.ImportDir("../../"+pkg, 0)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		for _, imp := range p.Imports {
			if rel, ok := strings.CutPrefix(imp, module); ok && !slices.Contains(allowed(pkg), rel) {
				t.Errorf("%s 不能导入 %s", pkg, rel)
			}
		}
	}
}
