package presets

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
)

// Preset 对应一个 agent 目录中的 preset.json。值中可以使用的模板变量见
// docs/architecture.md 第 5.6 节。
type Preset struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Provider   string            `json:"provider"`        // gate.Providers() 中的一个
	UI         string            `json:"ui"`              // terminal 或 web
	Cmd        []string          `json:"cmd"`             // terminal：在终端中运行的命令。web：容器的主进程
	Env        map[string]string `json:"env,omitempty"`   // 值中可以使用模板变量
	Files      map[string]string `json:"files,omitempty"` // 沙箱内的路径 → 内容，可以使用模板变量
	Web        *Web              `json:"web,omitempty"`   // UI 为 web 时必填
	Dockerfile []byte            `json:"-"`               // 来自同目录的 Dockerfile
}

type Web struct {
	Port         int    `json:"port"`         // 沙箱内网页服务的端口
	TokenPattern string `json:"tokenPattern"` // 从启动输出的每一行中提取令牌的正则表达式，必须有一个分组
	OpenPath     string `json:"openPath"`     // 网页界面的路径，以 / 开头并含 {token}，例如 /?token={token}
}

//go:embed */preset.json */Dockerfile
var files embed.FS

// providers 与 gate.Providers() 相同。presets 只使用标准库，不导入 gate。
var providers = []string{"anthropic", "openai", "deepseek"}

// All 返回全部预设，按 ID 排序。
func All() ([]Preset, error) { return load(files) }

// Get 返回 ID 为 id 的预设。
func Get(id string) (Preset, error) { return get(files, id) }

func get(fsys fs.FS, id string) (Preset, error) {
	all, err := load(fsys)
	if err != nil {
		return Preset{}, err
	}
	for _, p := range all {
		if p.ID == id {
			return p, nil
		}
	}
	return Preset{}, fmt.Errorf("presets: 没有找到预设 %q", id)
}

// load 读取 fsys 中每个目录的 preset.json 和 Dockerfile。fs.ReadDir 按名称排序，ID 等于目录名，所以结果按 ID 排序。
func load(fsys fs.FS) ([]Preset, error) {
	dirs, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var all []Preset
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		p, err := loadOne(fsys, d.Name())
		if err != nil {
			return nil, fmt.Errorf("presets: %s：%w", d.Name(), err)
		}
		all = append(all, p)
	}
	return all, nil
}

func loadOne(fsys fs.FS, dir string) (Preset, error) {
	data, err := fs.ReadFile(fsys, dir+"/preset.json")
	if err != nil {
		return Preset{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Preset
	if err := dec.Decode(&p); err != nil {
		return Preset{}, err
	}
	if p.Dockerfile, err = fs.ReadFile(fsys, dir+"/Dockerfile"); err != nil {
		return Preset{}, err
	}
	if p.ID != dir {
		return Preset{}, fmt.Errorf("id %q 与目录名不同", p.ID)
	}
	return p, check(p)
}

func check(p Preset) error {
	if p.Name == "" || len(p.Cmd) == 0 || len(p.Dockerfile) == 0 {
		return errors.New("name、cmd 和 Dockerfile 不能为空")
	}
	if !slices.Contains(providers, p.Provider) {
		return fmt.Errorf("不支持的模型供应商 %q", p.Provider)
	}
	switch p.UI {
	case "terminal":
		if p.Web != nil {
			return errors.New("terminal 型预设不能有 web")
		}
		return nil
	case "web":
	default:
		return fmt.Errorf("不支持的界面类型 %q", p.UI)
	}
	if p.Web == nil || p.Web.Port < 1 || p.Web.Port > 65535 {
		return errors.New("web 型预设必须有 web，并写明端口")
	}
	// cabin 把 openPath 拼在 http://127.0.0.1:<端口> 后面。不以 / 开头时，"@evil.example" 这样的值会改变主机。
	if !strings.HasPrefix(p.Web.OpenPath, "/") || !strings.Contains(p.Web.OpenPath, "{token}") {
		return errors.New("openPath 必须以 / 开头，并且含 {token}")
	}
	re, err := regexp.Compile(p.Web.TokenPattern)
	if err != nil {
		return fmt.Errorf("tokenPattern 不能编译：%w", err)
	}
	if re.NumSubexp() != 1 {
		return errors.New("tokenPattern 必须只有一个分组")
	}
	return nil
}
