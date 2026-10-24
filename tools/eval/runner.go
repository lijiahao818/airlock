package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lijiahao818/airlock/internal/advisor"
	"github.com/lijiahao818/airlock/internal/cabin"
	"github.com/lijiahao818/airlock/internal/gate"
	"github.com/lijiahao818/airlock/internal/vault"
)

// evalConfig 是一次评估的全部输入。
type evalConfig struct {
	Root     string                                // 仓库根目录，evals/ 的父目录
	Set      string                                // tuning、holdout 或 all
	Provider string                                // anthropic、deepseek 或空（自动选择）
	GetKey   func(provider string) (string, error) // 密钥来源，默认 vault.Get
}

// runEval 运行评估，返回进程退出码。
// 0 表示门槛全过，1 表示门槛不通过，2 表示装配失败或没有密钥。
func runEval(cfg evalConfig, stdout, stderr io.Writer) int {
	if cfg.GetKey == nil {
		cfg.GetKey = vault.Get
	}
	if err := checkFixtures(cfg.Root); err != nil {
		fmt.Fprintf(stderr, "评估无法运行：%v\n", err)
		return 2
	}
	sets := setNames
	if cfg.Set != "all" {
		sets = []string{cfg.Set}
	}
	loaded := map[string][]Case{}
	for _, name := range sets {
		cases, err := loadSet(cfg.Root, name)
		if err != nil {
			fmt.Fprintf(stderr, "评估无法运行：%v\n", err)
			return 2
		}
		loaded[name] = cases
	}

	provider, err := pickProvider(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "评估无法运行：%v\n", err)
		fmt.Fprintln(stderr, "没有可用的密钥时，评估不能调用模型。密钥保存在系统钥匙串（条目 airlock:<供应商>）里。")
		return 2
	}

	// 模型调用经过一个专用闸门实例，用占位符换真实密钥。
	var routed atomic.Int64
	g, err := gate.Start(gate.Config{
		Providers: []string{provider},
		Secret:    cfg.GetKey,
		Ask:       func(context.Context, gate.Event) bool { return false },
		OnEvent: func(e gate.Event) {
			if e.Rule == "route" {
				routed.Add(1)
			}
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "评估无法运行：%v\n", err)
		return 2
	}
	defer g.Close()

	// 评估的设置写在临时目录里，不读写用户的 advisor 设置。
	dataDir, err := os.MkdirTemp("", "airlock-eval-")
	if err != nil {
		fmt.Fprintf(stderr, "评估无法运行：创建临时目录失败：%v\n", err)
		return 2
	}
	defer os.RemoveAll(dataDir)

	adv, err := advisor.New(advisor.Config{
		DataDir: dataDir,
		BaseURL: "http://127.0.0.1:" + strconv.Itoa(g.Port()),
		Placeholder: func(p string) string {
			if p != provider {
				return ""
			}
			return g.Placeholder(p)
		},
		HasKey: func(p string) bool { return p == provider },
	})
	if err != nil {
		fmt.Fprintf(stderr, "评估无法运行：%v\n", err)
		return 2
	}
	if err := adv.SetSettings(advisor.Settings{Enabled: true, NoticeShown: true, Provider: provider}); err != nil {
		fmt.Fprintf(stderr, "评估无法运行：%v\n", err)
		return 2
	}

	model, effort, _ := adv.ModelConfig(provider)
	fmt.Fprintf(stdout, "评估报告\n供应商：%s\n模型：%s（effort：%s）\n", provider, model, effort)
	failed, promptVersion := runSets(adv, sets, loaded, g.Placeholder(provider), stdout)
	if promptVersion == "" {
		promptVersion = "未知（全部调用失败）"
	}
	fmt.Fprintf(stdout, "提示词版本：%s\n经闸门转发：%d 次\n", promptVersion, routed.Load())
	if failed == 0 {
		fmt.Fprintln(stdout, "门槛判定：通过（高风险召回率 100%，误放行 0）")
	} else {
		fmt.Fprintln(stdout, "门槛判定：不通过（高风险召回率低于 100%，或存在误放行）")
	}
	return failed
}

// runSets 逐集调用、评分并打印报告，返回退出码和提示词版本。
// ph 是本实例的占位符，打印前从错误文本中移除。
func runSets(adv cabin.Explainer, sets []string, loaded map[string][]Case, ph string, stdout io.Writer) (int, string) {
	failed := 0
	promptVersion := ""
	for _, name := range sets {
		outs := runSet(adv, name, loaded[name])
		for i := range outs {
			outs[i].Err = redact(outs[i].Err, ph)
		}
		if promptVersion == "" {
			promptVersion = firstPromptVersion(outs)
		}
		m := score(outs)
		if !m.passed() {
			failed = 1
		}
		reportSet(stdout, name, m, outs)
	}
	return failed, promptVersion
}

// redact 把错误文本里的占位符换成固定文字。报告不含占位符和密钥。
func redact(err error, ph string) error {
	if err == nil || ph == "" || !strings.Contains(err.Error(), ph) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), ph, "[占位符]"))
}

// evalWorkers 是并发调用模型的数量。
const evalWorkers = 4

// runSet 用并发池逐条调用解释器，返回与 cases 对齐的输出。
// 每条用例独立的运行 ID，绕开解释缓存和调用次数上限。
// 超时的用例换一个运行 ID 重试一次。超时是网络延迟，不是判断质量。
func runSet(adv cabin.Explainer, name string, cases []Case) []outcome {
	outs := make([]outcome, len(cases))
	var wg sync.WaitGroup
	sem := make(chan struct{}, evalWorkers)
	for i, c := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c Case) {
			defer wg.Done()
			defer func() { <-sem }()
			runID := fmt.Sprintf("%s-%03d", name, i)
			exp, err := adv.Explain(context.Background(), runID, c.Meta)
			if isTimeout(err) {
				exp, err = adv.Explain(context.Background(), runID+"-retry", c.Meta)
			}
			outs[i] = outcome{Case: c, Got: exp, Err: err}
		}(i, c)
	}
	wg.Wait()
	return outs
}

// isTimeout 判断错误是不是调用超时。
func isTimeout(err error) bool {
	return err != nil &&
		(errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded"))
}

// firstPromptVersion 返回第一条成功输出里的提示词版本。
func firstPromptVersion(outs []outcome) string {
	for _, o := range outs {
		if o.Err == nil && o.Got.PromptVersion != "" {
			return o.Got.PromptVersion
		}
	}
	return ""
}

// pickProvider 选择评估用的供应商。
// 指定了供应商时，它必须有密钥。只有一家有密钥时自动选它。
func pickProvider(cfg evalConfig) (string, error) {
	has := func(p string) bool {
		_, err := cfg.GetKey(p)
		return err == nil
	}
	if cfg.Provider != "" {
		if !has(cfg.Provider) {
			return "", fmt.Errorf("-provider 指定的 %s 没有密钥", cfg.Provider)
		}
		return cfg.Provider, nil
	}
	ha, hd := has("anthropic"), has("deepseek")
	switch {
	case !ha && !hd:
		return "", errors.New("系统钥匙串里没有 Anthropic 或 DeepSeek 的密钥")
	case ha && !hd:
		return "anthropic", nil
	case !ha && hd:
		return "deepseek", nil
	}
	return "", errors.New("两家的密钥都有，请用 -provider 选择一家")
}

// reportSet 打印一个集合的评分和不匹配的用例。
func reportSet(w io.Writer, name string, m metrics, outs []outcome) {
	fmt.Fprintf(w, "\n%s（%d 条）\n", setNameLabel(name), m.Total)
	fmt.Fprintf(w, "  准确率        %d/%d（%.1f%%）\n", m.Matches, m.Total, m.Accuracy*100)
	fmt.Fprintf(w, "  高风险召回率  %d/%d（%.1f%%）\n", m.HighHits, m.HighTotal, m.HighRecall*100)
	fmt.Fprintf(w, "  误放行数      %d\n", m.Misallow)
	fmt.Fprintf(w, "  要点覆盖      %d/%d\n", m.PointsHit, m.PointsAll)
	fmt.Fprintf(w, "  调用失败      %d\n", m.Failures)
	for _, o := range outs {
		if o.Err != nil {
			fmt.Fprintf(w, "  失败：%s：%v\n", o.Case.Name, o.Err)
			continue
		}
		e := o.Case.Expect
		if o.Got.RiskLevel == e.RiskLevel && o.Got.SuggestAllow == e.SuggestAllow {
			continue
		}
		fmt.Fprintf(w, "  不匹配：%s：期望 %s/建议放行 %v，得到 %s/建议放行 %v\n",
			o.Case.Name, e.RiskLevel, e.SuggestAllow, o.Got.RiskLevel, o.Got.SuggestAllow)
	}
}

func setNameLabel(name string) string {
	switch name {
	case "tuning":
		return "调优集"
	case "holdout":
		return "保留集"
	}
	return name
}
