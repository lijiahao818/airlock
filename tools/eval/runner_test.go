package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// validRoot 建一个只含一条高风险用例的最小评估集。
func validRoot(t *testing.T) string {
	t.Helper()
	root := writeEval(t, "tuning",
		`{"name":"a","meta":{"agent":"x","host":"a.example","port":443,"method":"CONNECT","rule":"hold"},"expect":{"risk_level":"high","suggest_allow":false}}`+"\n")
	writeSample(t, root, "s.txt", markedSample)
	return root
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
