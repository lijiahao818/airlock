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
| [#17](https://github.com/lijiahao818/airlock/pull/17) | 无法判断、无法判断 | 两份报告都缺架构文档内容和静态检查输出。 |
| [#19](https://github.com/lijiahao818/airlock/pull/19) | 不通过、无法判断、无法判断、无法判断、无法判断、无法判断 | 首份报告指出安全核心模块缺单元测试。后续五份报告缺静态检查输出。 |
| [#22](https://github.com/lijiahao818/airlock/pull/22) | 无法判断 | AC-7 缺少触发配置的证据。 |
| [#42](https://github.com/lijiahao818/airlock/pull/42) | 无法判断、通过、通过、通过 | 首份报告中，模型没有给出多数验收标准的结论。 |
| [#43](https://github.com/lijiahao818/airlock/pull/43) | 不通过、通过、通过 | 首份报告对 AC-6 和 AC-11 的有意偏离判不通过。 |
| [#44](https://github.com/lijiahao818/airlock/pull/44) | 无法判断、无法判断 | Windows 凭据管理器的往返测试只在作者本地运行。 |
| [#45](https://github.com/lijiahao818/airlock/pull/45) | 不通过、无法判断、不通过、不通过 | 首份报告判 schema 校验不完整。作者按审阅意见修改，审阅人复核后合并。 |
| [#52](https://github.com/lijiahao818/airlock/pull/52) | 不通过、不通过、通过 | 首份报告缺 Review 态 Start 的拒绝测试。补测后通过。 |
| [#55](https://github.com/lijiahao818/airlock/pull/55) | 无法判断、通过 | 首份报告无法确认嵌套的 Findings 字段会不会输出 null。 |
| [#59](https://github.com/lijiahao818/airlock/pull/59) | 不通过、无法判断 | 首份报告判前端手写类型不通过。CI 没有前端检查输出。 |
| [#60](https://github.com/lijiahao818/airlock/pull/60) | 无法判断、无法判断 | fakeToken 的值和前端三项检查只有作者本机结果。审阅人已复核确认。 |
| [#62](https://github.com/lijiahao818/airlock/pull/62) | 无法判断、无法判断、无法判断 | 四条验收标准依赖真实 Docker 实测。审阅人复跑测试后确认。 |
| [#63](https://github.com/lijiahao818/airlock/pull/63) | 无法判断 | CI 没有前端作业，AC-12 只有作者本地的验证结果。 |

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
| 10-24 | 19 | 38%（7/18） | 0 | [#15](https://github.com/lijiahao818/airlock/pull/15)、[#17](https://github.com/lijiahao818/airlock/pull/17)、[#19](https://github.com/lijiahao818/airlock/pull/19)、[#22](https://github.com/lijiahao818/airlock/pull/22)、[#40](https://github.com/lijiahao818/airlock/pull/40)、[#42](https://github.com/lijiahao818/airlock/pull/42)、[#43](https://github.com/lijiahao818/airlock/pull/43)、[#44](https://github.com/lijiahao818/airlock/pull/44)、[#45](https://github.com/lijiahao818/airlock/pull/45)、[#46](https://github.com/lijiahao818/airlock/pull/46)、[#48](https://github.com/lijiahao818/airlock/pull/48)、[#49](https://github.com/lijiahao818/airlock/pull/49)、[#50](https://github.com/lijiahao818/airlock/pull/50)、[#51](https://github.com/lijiahao818/airlock/pull/51)、[#52](https://github.com/lijiahao818/airlock/pull/52)、[#53](https://github.com/lijiahao818/airlock/pull/53)、[#55](https://github.com/lijiahao818/airlock/pull/55)、[#59](https://github.com/lijiahao818/airlock/pull/59)、[#60](https://github.com/lijiahao818/airlock/pull/60) |
| 10-25 | 3 | 33%（1/3） | 0 | [#62](https://github.com/lijiahao818/airlock/pull/62)、[#63](https://github.com/lijiahao818/airlock/pull/63)、[#65](https://github.com/lijiahao818/airlock/pull/65) |
