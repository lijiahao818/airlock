package gate

import (
	"errors"
	"slices"
)

// 严格档位只放行模型接口。
var modelHosts = []string{
	"api.anthropic.com",
	"api.openai.com",
	"api.deepseek.com",
}

// 标准档位再放行常用包管理源。理由见 docs/security.md 第 7 节。
var packageHosts = []string{
	"registry.npmjs.org",
	"registry.yarnpkg.com",
	"pypi.org",
	"files.pythonhosted.org",
	"proxy.golang.org",
	"sum.golang.org",
	"index.crates.io",
	"static.crates.io",
	"repo.maven.apache.org",
}

// 宽松档位再放行常用开发网站。
var devHosts = []string{
	"github.com",
	"api.github.com",
	"codeload.github.com",
	"raw.githubusercontent.com",
	"objects.githubusercontent.com",
	"pkg.go.dev",
	"docs.python.org",
	"developer.mozilla.org",
	"stackoverflow.com",
}

// Tiers 返回全部安全档位。
func Tiers() []string { return []string{"strict", "standard", "permissive"} }

// TierHosts 返回档位自动放行的主机。
func TierHosts(tier string) ([]string, error) {
	switch tier {
	case "strict":
		return slices.Clone(modelHosts), nil
	case "standard":
		return slices.Concat(modelHosts, packageHosts), nil
	case "permissive":
		return slices.Concat(modelHosts, packageHosts, devHosts), nil
	}
	return nil, errors.New("gate: 未知的安全档位 " + tier)
}
