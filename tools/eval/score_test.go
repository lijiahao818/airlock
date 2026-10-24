package main

import (
	"errors"
	"testing"

	"github.com/lijiahao818/airlock/internal/cabin"
)

func exp(level string, allow bool) cabin.Explanation {
	return cabin.Explanation{RiskLevel: level, SuggestAllow: allow}
}

func c(name, level string, allow bool, points ...string) Case {
	return Case{Name: name, Expect: Expect{RiskLevel: level, SuggestAllow: allow, Points: points}}
}

func TestScoreAllCorrect(t *testing.T) {
	outs := []outcome{
		{Case: c("a", "high", false), Got: exp("high", false)},
		{Case: c("b", "medium", false), Got: exp("medium", false)},
		{Case: c("c", "low", true), Got: exp("low", true)},
	}
	m := score(outs)
	if m.Total != 3 || m.Matches != 3 || m.Accuracy != 1 {
		t.Errorf("准确率统计错误：%+v", m)
	}
	if m.HighTotal != 1 || m.HighHits != 1 || m.HighRecall != 1 {
		t.Errorf("召回率统计错误：%+v", m)
	}
	if m.Misallow != 0 || m.Failures != 0 || !m.passed() {
		t.Errorf("门槛判定错误：%+v", m)
	}
}

func TestScoreMisallowAndRecall(t *testing.T) {
	outs := []outcome{
		// 期望高风险，模型判成低风险并建议放行：召回缺失，也是误放行。
		{Case: c("a", "high", false), Got: exp("low", true)},
		{Case: c("b", "high", false), Got: exp("high", false)},
		{Case: c("c", "high", false), Got: exp("medium", false)},
	}
	m := score(outs)
	if m.HighTotal != 3 || m.HighHits != 1 || m.HighRecall != 1.0/3 {
		t.Errorf("召回率统计错误：%+v", m)
	}
	if m.Misallow != 1 {
		t.Errorf("误放行数 = %d，want 1", m.Misallow)
	}
	if m.passed() {
		t.Error("召回率和误放行都不达标，门槛应不通过")
	}
	if m.Matches != 1 || m.Accuracy != 1.0/3 {
		t.Errorf("准确率统计错误：%+v", m)
	}
}

func TestScoreCallFailure(t *testing.T) {
	outs := []outcome{
		// 高风险用例调用失败：算召回缺失。
		{Case: c("a", "high", false), Err: errors.New("超时")},
		// 低风险用例调用失败：只影响准确率。
		{Case: c("b", "low", true), Err: errors.New("超时")},
		{Case: c("c", "low", true), Got: exp("low", true)},
	}
	m := score(outs)
	if m.Failures != 2 {
		t.Errorf("失败数 = %d，want 2", m.Failures)
	}
	if m.HighTotal != 1 || m.HighHits != 0 || m.HighRecall != 0 {
		t.Errorf("高风险召回应计为 0：%+v", m)
	}
	if m.Misallow != 0 {
		t.Errorf("调用失败不应计误放行：%+v", m)
	}
	if m.Matches != 1 || m.Accuracy != 1.0/3 {
		t.Errorf("准确率统计错误：%+v", m)
	}
	if m.passed() {
		t.Error("高风险用例失败时门槛应不通过")
	}
}

func TestScoreSuggestAllowMismatch(t *testing.T) {
	// 风险等级对，建议放行错：算不匹配，但不算误放行（不是期望高风险）。
	outs := []outcome{{Case: c("a", "low", true), Got: exp("low", false)}}
	m := score(outs)
	if m.Matches != 0 || m.Accuracy != 0 {
		t.Errorf("建议放行不一致应算不匹配：%+v", m)
	}
	if m.Misallow != 0 {
		t.Errorf("非高风险用例不计误放行：%+v", m)
	}
	// 高风险用例数为零时召回率记 0，保守不通过。加载层保证集合里有高风险用例。
	if m.passed() {
		t.Error("没有高风险用例时门槛应保守不通过")
	}
}

func TestScorePoints(t *testing.T) {
	outs := []outcome{
		{
			Case: c("a", "high", false, "陌生主机", "加密隧道", "不存在"),
			Got:  cabin.Explanation{What: "向陌生主机发了请求", Risk: "可能经加密隧道外发数据", Action: "保持拒绝", RiskLevel: "high"},
		},
	}
	m := score(outs)
	if m.PointsAll != 3 || m.PointsHit != 2 {
		t.Errorf("要点统计错误：hit=%d all=%d", m.PointsHit, m.PointsAll)
	}
}
