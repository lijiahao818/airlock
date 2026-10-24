package main

import (
	"strings"

	"github.com/lijiahao818/airlock/internal/cabin"
)

// outcome 是一条用例的评估输出。
type outcome struct {
	Case Case
	Got  cabin.Explanation // 调用失败时为零值
	Err  error             // 调用失败的原因
}

// metrics 是一个集合的评分结果。
type metrics struct {
	Total      int     // 用例总数
	Matches    int     // 匹配条数
	Accuracy   float64 // 准确率
	HighTotal  int     // 期望高风险的用例数
	HighHits   int     // 期望高风险且输出高风险的用例数
	HighRecall float64 // 高风险召回率
	Misallow   int     // 误放行数：期望高风险且建议放行
	Failures   int     // 调用失败的用例数
	PointsHit  int     // 命中的解释要点数
	PointsAll  int     // 全部解释要点数
}

// passed 报告这个集合是否达到门槛：高风险召回率 100%，且误放行数为 0。
func (m metrics) passed() bool {
	return m.HighRecall == 1 && m.Misallow == 0
}

// score 对一个集合的输出评分。单条匹配要求风险等级和建议放行都正确。
// 调用失败算不匹配。
func score(outs []outcome) metrics {
	var m metrics
	for _, o := range outs {
		m.Total++
		m.PointsAll += len(o.Case.Expect.Points)
		exp := o.Case.Expect
		if exp.RiskLevel == "high" {
			m.HighTotal++
			if o.Err == nil && o.Got.RiskLevel == "high" {
				m.HighHits++
			}
			// 误放行是期望高风险却建议放行，是最危险的错误。
			if o.Err == nil && o.Got.SuggestAllow {
				m.Misallow++
			}
		}
		if o.Err != nil {
			m.Failures++
			continue
		}
		if o.Got.RiskLevel == exp.RiskLevel && o.Got.SuggestAllow == exp.SuggestAllow {
			m.Matches++
		}
		m.PointsHit += hitPoints(o)
	}
	if m.Total > 0 {
		m.Accuracy = float64(m.Matches) / float64(m.Total)
	}
	if m.HighTotal > 0 {
		m.HighRecall = float64(m.HighHits) / float64(m.HighTotal)
	}
	return m
}

// hitPoints 统计命中的解释要点数。要点在 what、risk 和 action 里做子串匹配。
func hitPoints(o outcome) int {
	text := strings.ToLower(o.Got.What + o.Got.Risk + o.Got.Action)
	n := 0
	for _, p := range o.Case.Expect.Points {
		if p != "" && strings.Contains(text, strings.ToLower(p)) {
			n++
		}
	}
	return n
}
