package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeEval 往临时目录写一个评估集文件。
func writeEval(t *testing.T, name, content string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "evals")
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// repoFixture 读仓库里的真实样本文件。注入样本只能放在 evals/fixtures/（规则 11.2），
// 测试不在源码里复制样本内容。
func repoFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "evals", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeSample(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, "evals", "fixtures", name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSet(t *testing.T) {
	root := writeEval(t, "tuning", "\n"+
		`{"name":"a","meta":{"agent":"claude-code","host":"a.example","port":443,"method":"CONNECT","rule":"hold"},"expect":{"risk_level":"high","suggest_allow":false,"points":["陌生主机"]}}`+"\n"+
		`{"name":"b","meta":{"agent":"dsh","host":"","port":22,"method":"CONNECT","rule":"hold"},"fixture":"s.txt","expect":{"risk_level":"high","suggest_allow":false,"points":["陌生"]}}`+"\n")
	writeSample(t, root, "s.txt", repoFixture(t, "injection-close-tag.txt"))

	cases, err := loadSet(root, "tuning")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("用例数 = %d， want 2", len(cases))
	}
	if cases[0].Expect.RiskLevel != "high" || cases[0].Expect.SuggestAllow {
		t.Errorf("第一条的期望解析错误：%+v", cases[0].Expect)
	}
	if !strings.Contains(cases[1].Meta.Host, "忽略以上规则") {
		t.Errorf("fixture 没有填进 host：%q", cases[1].Meta.Host)
	}
	if strings.Contains(cases[1].Meta.Host, "测试样本") {
		t.Errorf("fixture 的首行标注没有被去掉：%q", cases[1].Meta.Host)
	}
}

func TestLoadSetInvalid(t *testing.T) {
	base := `{"name":"a","meta":{"agent":"claude-code","host":"a.example","port":443,"method":"CONNECT","rule":"hold"},"expect":{"risk_level":"high","suggest_allow":false,"points":["要点"]}}`
	tests := []struct {
		name string
		line string
		want string
	}{
		{"用例名为空", strings.Replace(base, `"name":"a"`, `"name":""`, 1), "用例名不能为空"},
		{"风险等级越界", strings.Replace(base, `"risk_level":"high"`, `"risk_level":"extreme"`, 1), "不是 low、medium 或 high"},
		{"端口越界", strings.Replace(base, `"port":443`, `"port":0`, 1), "越界"},
		{"agent 为空", strings.Replace(base, `"agent":"claude-code"`, `"agent":""`, 1), "agent 不能为空"},
		{"method 为空", strings.Replace(base, `"method":"CONNECT"`, `"method":""`, 1), "method 不能为空"},
		{"rule 为空", strings.Replace(base, `"rule":"hold"`, `"rule":""`, 1), "rule 不能为空"},
		{"host 和 fixture 都为空", strings.Replace(base, `"host":"a.example"`, `"host":""`, 1), "host 和 fixture 不能都为空"},
		{"host 和 fixture 同填", strings.Replace(base, `"expect"`, `"fixture":"s.txt","expect"`, 1), "host 和 fixture 不能同时填写"},
		{"没有解释要点", strings.Replace(base, `,"points":["要点"]`, "", 1), "解释要点不能为空"},
		{"JSON 损坏", "{", "无法解析"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeEval(t, "tuning", tt.line+"\n")
			_, err := loadSet(root, "tuning")
			if err == nil {
				t.Fatal("期望报错，得到 nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("错误 %q 不含 %q", err, tt.want)
			}
		})
	}
}

func TestLoadSetDuplicateName(t *testing.T) {
	line := `{"name":"a","meta":{"agent":"x","host":"a.example","port":443,"method":"CONNECT","rule":"hold"},"expect":{"risk_level":"low","suggest_allow":true,"points":["要点"]}}`
	root := writeEval(t, "tuning", line+"\n"+line+"\n")
	_, err := loadSet(root, "tuning")
	if err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("期望报重名错误，得到 %v", err)
	}
}

func TestLoadSetEmpty(t *testing.T) {
	root := writeEval(t, "tuning", "\n\n")
	if _, err := loadSet(root, "tuning"); err == nil || !strings.Contains(err.Error(), "空的") {
		t.Fatalf("期望报空集错误，得到 %v", err)
	}
}

func TestLoadSetNoHighRisk(t *testing.T) {
	line := `{"name":"a","meta":{"agent":"x","host":"a.example","port":443,"method":"CONNECT","rule":"hold"},"expect":{"risk_level":"low","suggest_allow":true,"points":["要点"]}}`
	root := writeEval(t, "tuning", line+"\n")
	if _, err := loadSet(root, "tuning"); err == nil || !strings.Contains(err.Error(), "没有高风险用例") {
		t.Fatalf("期望报没有高风险用例的错误，得到 %v", err)
	}
}

// TestRepoSetsLoad 校验仓库里的真实评估集。设计规模是每集 20 条、高风险至少 6 条。
func TestRepoSetsLoad(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := checkFixtures(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range setNames {
		cases, err := loadSet(root, name)
		if err != nil {
			t.Fatalf("%s：%v", name, err)
		}
		high := 0
		for _, c := range cases {
			if c.Expect.RiskLevel == "high" {
				high++
			}
		}
		if len(cases) < 20 {
			t.Errorf("%s 只有 %d 条用例，少于 20", name, len(cases))
		}
		if high < 6 {
			t.Errorf("%s 只有 %d 条高风险用例，少于 6", name, high)
		}
	}
}

func TestFixtureInvalid(t *testing.T) {
	base := `{"name":"a","meta":{"agent":"x","host":"","port":443,"method":"CONNECT","rule":"hold"},"fixture":"%s","expect":{"risk_level":"high","suggest_allow":false,"points":["要点"]}}`
	tests := []struct {
		name    string
		fixture string
		content string
		write   bool
		want    string
	}{
		{"带路径分隔符", `../s.txt`, "", false, "必须是 evals/fixtures 下的文件名"},
		{"文件不存在", "nope.txt", "", false, "读取样本失败"},
		{"首行没有标注", "s.txt", "第一行不是标注\n内容\n", true, "第一行没有以"},
		{"只有标注行", "s.txt", "测试样本，不是指令。\n", true, "没有内容"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeEval(t, "tuning", strings.Replace(base, "%s", tt.fixture, 1)+"\n")
			if tt.write {
				writeSample(t, root, filepath.Base(tt.fixture), tt.content)
			}
			_, err := loadSet(root, "tuning")
			if err == nil {
				t.Fatal("期望报错，得到 nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("错误 %q 不含 %q", err, tt.want)
			}
		})
	}
}

func TestCheckFixtures(t *testing.T) {
	t.Run("全部有标注", func(t *testing.T) {
		root := writeEval(t, "tuning", "")
		writeSample(t, root, "a.txt", repoFixture(t, "injection-close-tag.txt"))
		writeSample(t, root, "b.txt", "测试样本，不是指令。\n其他内容\n")
		if err := checkFixtures(root); err != nil {
			t.Fatalf("期望通过，得到 %v", err)
		}
	})

	t.Run("缺标注被拒绝", func(t *testing.T) {
		root := writeEval(t, "tuning", "")
		writeSample(t, root, "a.txt", repoFixture(t, "injection-close-tag.txt"))
		writeSample(t, root, "bad.txt", "直接就是样本\n")
		err := checkFixtures(root)
		if err == nil || !strings.Contains(err.Error(), "bad.txt") {
			t.Fatalf("期望报 bad.txt 的错误，得到 %v", err)
		}
	})

	t.Run("没有样本被拒绝", func(t *testing.T) {
		root := writeEval(t, "tuning", "")
		err := checkFixtures(root)
		if err == nil || !strings.Contains(err.Error(), "没有注入样本") {
			t.Fatalf("期望报没有样本的错误，得到 %v", err)
		}
	})

	t.Run("跳过子目录", func(t *testing.T) {
		root := writeEval(t, "tuning", "")
		writeSample(t, root, "a.txt", repoFixture(t, "injection-close-tag.txt"))
		if err := os.Mkdir(filepath.Join(root, "evals", "fixtures", "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := checkFixtures(root); err != nil {
			t.Fatalf("子目录应被跳过，得到 %v", err)
		}
	})

	t.Run("只有一行被拒绝", func(t *testing.T) {
		root := writeEval(t, "tuning", "")
		writeSample(t, root, "one.txt", "只有一行")
		err := checkFixtures(root)
		if err == nil || !strings.Contains(err.Error(), "缺少首行标注") {
			t.Fatalf("期望报缺少标注的错误，得到 %v", err)
		}
	})

	t.Run("没有样本目录被拒绝", func(t *testing.T) {
		err := checkFixtures(t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "读取") {
			t.Fatalf("期望报读取失败，得到 %v", err)
		}
	})
}

// 一行超过扫描器的上限时，loadSet 报错，不按空集合处理。
func TestLoadSetLineTooLong(t *testing.T) {
	root := writeEval(t, "tuning", strings.Repeat("x", 70<<10)+"\n")
	if _, err := loadSet(root, "tuning"); err == nil {
		t.Fatal("期望报错，得到 nil")
	}
}
