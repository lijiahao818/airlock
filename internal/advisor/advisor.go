package advisor

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

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

type Advisor struct{}

var _ cabin.Explainer = (*Advisor)(nil)

func New(cfg Config) (*Advisor, error) { return nil, errNotImplemented }

func (a *Advisor) Settings() Settings { return Settings{} }

func (a *Advisor) SetSettings(s Settings) error { return errNotImplemented }

func (a *Advisor) Explain(ctx context.Context, runID string, m cabin.HoldMeta) (cabin.Explanation, error) {
	return cabin.Explanation{}, errNotImplemented
}
