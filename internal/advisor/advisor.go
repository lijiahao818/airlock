package advisor

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/lijiahao818/airlock/internal/cabin"
)

var errNotImplemented = errors.New("advisor: 未实现")

//go:embed prompt.md
var promptMD []byte

//go:embed config.json
var configJSON []byte

// promptVersion 是提示词的版本，取 prompt.md 的 SHA-256 前 12 位。
var promptVersion = func() string {
	sum := sha256.Sum256(promptMD)
	return hex.EncodeToString(sum[:])[:12]
}()

// providerConfig 是一个供应商的模型配置。
type providerConfig struct {
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	MaxTokens int64  `json:"max_tokens"`
}

// parseModels 解析模型配置。两家供应商都必须写明具体的模型 ID 和 effort。
func parseModels(b []byte) (map[string]providerConfig, error) {
	var models map[string]providerConfig
	if err := json.Unmarshal(b, &models); err != nil {
		return nil, fmt.Errorf("advisor: 解析 config.json 失败：%w", err)
	}
	for _, name := range []string{"anthropic", "deepseek"} {
		pc, ok := models[name]
		if !ok || pc.Model == "" || pc.Effort == "" {
			return nil, fmt.Errorf("advisor: config.json 没有写明 %s 的模型 ID 和 effort", name)
		}
	}
	return models, nil
}

type Settings struct {
	Enabled     bool   `json:"enabled"`     // 用户开启了安全副驾
	Provider    string `json:"provider"`    // 用户添加了两家的密钥时选择的一家
	NoticeShown bool   `json:"noticeShown"` // 用户已经看过数据发送说明
}

type Config struct {
	DataDir     string
	BaseURL     string                       // 安全副驾专用闸门的地址
	Placeholder func(provider string) string // 安全副驾专用闸门签发的占位符
	HasKey      func(provider string) bool
}

// settingsFile 是设置在 DataDir 下的文件名。
const settingsFile = "advisor.json"

type Advisor struct {
	cfg      Config
	models   map[string]providerConfig
	mu       sync.Mutex // 保护 settings
	settings Settings
}

var _ cabin.Explainer = (*Advisor)(nil)

func (a *Advisor) settingsPath() string {
	return filepath.Join(a.cfg.DataDir, settingsFile)
}

func New(cfg Config) (*Advisor, error) {
	switch {
	case cfg.DataDir == "":
		return nil, errors.New("advisor: 缺少 DataDir")
	case cfg.BaseURL == "":
		return nil, errors.New("advisor: 缺少 BaseURL")
	case cfg.Placeholder == nil:
		return nil, errors.New("advisor: 缺少 Placeholder")
	case cfg.HasKey == nil:
		return nil, errors.New("advisor: 缺少 HasKey")
	}
	models, err := parseModels(configJSON)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("advisor: 创建数据目录失败：%w", err)
	}
	a := &Advisor{cfg: cfg, models: models}
	b, err := os.ReadFile(a.settingsPath())
	if err == nil {
		if err := json.Unmarshal(b, &a.settings); err != nil {
			return nil, fmt.Errorf("advisor: 解析 %s 失败：%w", settingsFile, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("advisor: 读取 %s 失败：%w", settingsFile, err)
	}
	return a, nil
}

func (a *Advisor) Settings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

func (a *Advisor) SetSettings(s Settings) error {
	switch s.Provider {
	case "", "anthropic", "deepseek":
	default:
		return fmt.Errorf("advisor: 未知供应商 %q", s.Provider)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.WriteFile(a.settingsPath(), b, 0o600); err != nil {
		return fmt.Errorf("advisor: 写入 %s 失败：%w", settingsFile, err)
	}
	a.settings = s
	return nil
}

func (a *Advisor) Explain(ctx context.Context, runID string, m cabin.HoldMeta) (cabin.Explanation, error) {
	return cabin.Explanation{}, errNotImplemented
}
