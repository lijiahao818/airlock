package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/lijiahao818/airlock/internal/cabin"
)

// fakeExplainer 按主机返回固定解释，不调用模型。
type fakeExplainer struct {
	mu      sync.Mutex
	runIDs  map[string]bool
	byHost  map[string]cabin.Explanation
	errHost map[string]error
}

func (f *fakeExplainer) Explain(_ context.Context, runID string, m cabin.HoldMeta) (cabin.Explanation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runIDs == nil {
		f.runIDs = map[string]bool{}
	}
	f.runIDs[runID] = true
	if err, ok := f.errHost[m.Host]; ok {
		return cabin.Explanation{}, err
	}
	return f.byHost[m.Host], nil
}

func TestRunSet(t *testing.T) {
	n := 12
	cases := make([]Case, n)
	for i := range cases {
		level := "medium"
		if i%3 == 0 {
			level = "high"
		}
		cases[i] = Case{
			Name:   "c" + strings.Repeat("x", i+1), // 长度不同，验证对齐
			Meta:   cabin.HoldMeta{Host: "h.example", Port: 443, Method: "CONNECT", Rule: "hold"},
			Expect: Expect{RiskLevel: level, SuggestAllow: false},
		}
	}
	f := &fakeExplainer{byHost: map[string]cabin.Explanation{
		"h.example": {RiskLevel: "medium", SuggestAllow: false, PromptVersion: "fake-version"},
	}}
	outs := runSet(f, "tuning", cases)
	if len(outs) != n {
		t.Fatalf("输出条数 = %d，want %d", len(outs), n)
	}
	for i, o := range outs {
		if o.Case.Name != cases[i].Name {
			t.Fatalf("第 %d 条错位：%s != %s", i, o.Case.Name, cases[i].Name)
		}
		if o.Err != nil {
			t.Fatalf("第 %d 条不应失败：%v", i, o.Err)
		}
	}
	if len(f.runIDs) != n {
		t.Errorf("运行 ID 数 = %d，want %d，应每条用例独立", len(f.runIDs), n)
	}
	if got := firstPromptVersion(outs); got != "fake-version" {
		t.Errorf("提示词版本 = %q", got)
	}
	// 三分之一期望高风险但全部输出 medium，门槛应不通过。
	if score(outs).passed() {
		t.Error("高风险全部漏判时门槛应不通过")
	}
}

func TestRunSetFailure(t *testing.T) {
	cases := []Case{
		{Name: "ok", Meta: cabin.HoldMeta{Host: "a.example"}, Expect: Expect{RiskLevel: "low", SuggestAllow: true}},
		{Name: "bad", Meta: cabin.HoldMeta{Host: "b.example"}, Expect: Expect{RiskLevel: "low", SuggestAllow: true}},
	}
	f := &fakeExplainer{
		byHost:  map[string]cabin.Explanation{"a.example": {RiskLevel: "low", SuggestAllow: true}},
		errHost: map[string]error{"b.example": errors.New("超时")},
	}
	outs := runSet(f, "holdout", cases)
	if outs[0].Err != nil || outs[1].Err == nil {
		t.Fatalf("失败传递错误：%+v %+v", outs[0], outs[1])
	}
	if m := score(outs); m.Failures != 1 || m.Matches != 1 {
		t.Errorf("评分统计错误：%+v", m)
	}
}

// validRoot 建一个只含一条高风险用例的最小评估集。
func validRoot(t *testing.T) string {
	t.Helper()
	root := writeEval(t, "tuning",
		`{"name":"a","meta":{"agent":"x","host":"a.example","port":443,"method":"CONNECT","rule":"hold"},"expect":{"risk_level":"high","suggest_allow":false}}`+"\n")
	writeSample(t, root, "s.txt", markedSample)
	return root
}

func TestPickProvider(t *testing.T) {
	noKey := func(string) (string, error) { return "", errors.New("vault: 没有找到密钥") }
	deepseekOnly := func(p string) (string, error) {
		if p == "deepseek" {
			return "fake-key", nil
		}
		return "", errors.New("vault: 没有找到密钥")
	}
	both := func(string) (string, error) { return "fake-key", nil }

	tests := []struct {
		name     string
		provider string
		getKey   func(string) (string, error)
		want     string
		wantErr  string
	}{
		{"都没有", "", noKey, "", "没有"},
		{"单家自动选择", "", deepseekOnly, "deepseek", ""},
		{"指定有密钥的供应商", "deepseek", both, "deepseek", ""},
		{"指定没有密钥的供应商", "anthropic", deepseekOnly, "", "没有密钥"},
		{"两家都有且未指定", "", both, "", "选择一家"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pickProvider(evalConfig{Provider: tt.provider, GetKey: tt.getKey})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("期望报含 %q 的错误，得到 %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("选中 %q，want %q", got, tt.want)
			}
		})
	}
}

// 这些测试只覆盖供应商选择和退出码，不调用模型。
func TestRunEvalNoKeys(t *testing.T) {
	root := validRoot(t)
	var out, errOut bytes.Buffer
	code := runEval(evalConfig{
		Root:   root,
		Set:    "tuning",
		GetKey: func(string) (string, error) { return "", errors.New("vault: 没有找到密钥") },
	}, &out, &errOut)
	if code != 2 {
		t.Fatalf("退出码 = %d，want 2", code)
	}
	if !strings.Contains(errOut.String(), "没有可用的密钥") {
		t.Fatalf("错误输出应说明密钥缺失：%q", errOut.String())
	}
}

func TestRunEvalAmbiguousProvider(t *testing.T) {
	root := validRoot(t)
	var out, errOut bytes.Buffer
	code := runEval(evalConfig{
		Root:   root,
		Set:    "tuning",
		GetKey: func(string) (string, error) { return "fake-key", nil },
	}, &out, &errOut)
	if code != 2 {
		t.Fatalf("退出码 = %d，want 2", code)
	}
	if !strings.Contains(errOut.String(), "选择一家") {
		t.Fatalf("错误输出应要求选择供应商：%q", errOut.String())
	}
}

func TestRunEvalMissingSet(t *testing.T) {
	root := validRoot(t)
	var out, errOut bytes.Buffer
	code := runEval(evalConfig{
		Root:   root,
		Set:    "holdout", // validRoot 只写了 tuning.jsonl
		GetKey: func(string) (string, error) { return "fake-key", nil },
	}, &out, &errOut)
	if code != 2 {
		t.Fatalf("退出码 = %d，want 2", code)
	}
	if !strings.Contains(errOut.String(), "读取评估集") {
		t.Fatalf("错误输出应说明读取失败：%q", errOut.String())
	}
}

func TestRunBadFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"集合名越界", []string{"-set", "bogus"}, "-set 只能是"},
		{"供应商名越界", []string{"-provider", "openai"}, "-provider 只能是"},
		{"多余参数", []string{"extra"}, "未知的参数"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run(tt.args, &out, &errOut); code != 2 {
				t.Fatalf("退出码 = %d，want 2", code)
			}
			if !strings.Contains(errOut.String(), tt.want) {
				t.Fatalf("错误输出应含 %q：%q", tt.want, errOut.String())
			}
		})
	}
}
