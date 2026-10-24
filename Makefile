# 评估安全副驾。见 docs/architecture.md 第 5.13 节和 CLAUDE.md 第 13 节。
.PHONY: eval
eval:
	go run ./tools/eval -root .
