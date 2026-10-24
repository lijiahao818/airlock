# AI 工作流

本文档说明团队怎样使用 AI、AI 评估的结果和每天的效率指标。AI 的行为边界见 [CLAUDE.md](../CLAUDE.md) 第 11 节。

## 1. AI 的使用方式

团队在三个位置使用 AI：

| 位置 | 用途 | 决定权 |
|---|---|---|
| 开发过程 | 成员用开发用 AI 调研竞品、做技术验证、起草文档、编写代码和测试。 | 成员审阅并决定是否采用。 |
| CI 验收 | AI 验收评估检查每个 PR 是否满足 Issue 的验收标准。 | 评估结论是必需检查。审阅人可以推翻评估结论，并留下记录。 |
| 产品功能 | 安全副驾向用户解释风险。 | 安全副驾不做放行决定。 |

### 1.1 开发过程

技术方案先由 AI 做实测，再写入 ADR。例如，沙箱的出网和入站链路经过 17 项实测，结果见 [security.md](security.md#6-验证记录)。

### 1.2 CI 中的 AI 验收评估

| 项目 | 位置 |
|---|---|
| 评估工具 | `tools/ai-eval` |
| 模型 ID 和 effort | `.github/ai-eval/config.json` |
| 评估提示词 | `.github/ai-eval/prompt.md` |
| 工作流 | `.github/workflows/ci.yml` |

评估的输入、输出和判定规则见 [CLAUDE.md](../CLAUDE.md) 第 12 节。评估工具把报告发布为 PR 评论。

## 2. AI 评估摘要

| PR | 评估报告（按时间顺序） | 说明 |
|---|---|---|
| [#3](https://github.com/lijiahao818/airlock/pull/3) | 无 | CI 工作流合并前提交，免于 AI 评估。 |
| [#5](https://github.com/lijiahao818/airlock/pull/5) | 无 | CI 工作流合并前提交，免于 AI 评估。 |
| [#8](https://github.com/lijiahao818/airlock/pull/8) | 无法判断、不通过、通过、通过、无法判断、通过 | 见下文。 |

PR #8 的评估过程：

- 第一份报告为"无法判断"。原因是 CI 使用的 API key 没有绑定 workspace，模型调用失败。
- 第二份报告判定 AC-9 不通过。报告指出：带有 `ai-eval-override` 标签的 PR 再次推送代码时，评估失败仍会阻断合并。报告还指出：添加任意标签会让失败的评估检查变为通过。
- 第三、四、六份报告判定通过。第五份报告为"无法判断"。
- 10-23 的首次通过率为 0%，原因是第一份报告的模型调用失败，不是代码不满足验收标准。

## 3. 效率指标

统计口径：

- 按合并日期分组，时区为 UTC+8。
- 首次通过率只看每个 PR 的第一份评估报告。
- 推翻次数是带 `ai-eval-override` 标签的已合并 PR 数。一个 PR 多次推翻只算一次。

数据由下列命令生成：

```bash
gh pr list --state merged --limit 500 --json number,mergedAt,labels,comments > prs.json
go run ./tools/ai-eval stats -prs prs.json
```

`main` 每次更新后，CI 的"效率指标"作业运行同样的命令，把表格和折线图写入这次运行的摘要页面。查看方法：在仓库的 Actions 页面选择 CI 工作流，打开 `main` 分支最近一次 push 触发的运行，查看 Summary。

| 日期 | 合并的 PR 数 | AI 评估首次通过率 | 推翻次数 | PR |
|---|---|---|---|---|
| 10-23 | 5 | 50%（1/2） | 0 | [#3](https://github.com/lijiahao818/airlock/pull/3)、[#5](https://github.com/lijiahao818/airlock/pull/5)、[#8](https://github.com/lijiahao818/airlock/pull/8)、[#11](https://github.com/lijiahao818/airlock/pull/11)、[#13](https://github.com/lijiahao818/airlock/pull/13) |
| 10-24 | 5 | 25%（1/4） | 0 | [#15](https://github.com/lijiahao818/airlock/pull/15)、[#17](https://github.com/lijiahao818/airlock/pull/17)、[#19](https://github.com/lijiahao818/airlock/pull/19)、[#22](https://github.com/lijiahao818/airlock/pull/22)、[#40](https://github.com/lijiahao818/airlock/pull/40) |
