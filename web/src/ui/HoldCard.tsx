import type * as cabin from "../generated/cabin";
import { suggestionLabel } from "./advisor.ts";
import { riskLevelName } from "./names.ts";
import Icon from "./Icon.tsx";

// 拦截卡片。每张卡片回答三个问题：发生了什么、风险多大、用户可以怎么做（CLAUDE.md 第 15 节）。
// 解释文字来自规则模板或模型，都按不可信数据处理：只用纯文本显示，不渲染链接和 HTML。
export default function HoldCard(props: {
  hold: cabin.Hold;
  busy: boolean;
  error: string;
  onAllow: () => void;
  onDeny: () => void;
}) {
  const h = props.hold;
  const e = h.explanation;
  // CONNECT 请求只有主机和端口，模型路由上的请求有路径。
  const target = h.path ? `${h.host}${h.path}` : `${h.host}:${h.port}`;
  return (
    <section className="card holdcard" aria-label="拦截卡片">
      <div className="holdcard-head">
        <Icon name="alert" />
        <h3 className="grow">拦截了一次出网请求</h3>
        <span className={`badge badge-risk-${e.riskLevel}`}>
          {riskLevelName(e.riskLevel)}风险
        </span>
      </div>
      <div className="holdcard-body">
        <p className="hold-target">
          {h.method} {target}
        </p>
        <dl className="hold-qa">
          <dt>发生了什么</dt>
          <dd>{e.what}</dd>
          <dt>风险多大</dt>
          <dd>{e.risk}</dd>
          <dt>你可以怎么做</dt>
          <dd>{e.action}</dd>
        </dl>
        {e.aiUnavailable && <p className="notice-line">AI 解释暂不可用，上面是规则说明。</p>}
        {!e.aiUnavailable && e.source === "ai" && (
          <p className="muted">这段解释由 AI 安全副驾生成。</p>
        )}
        <p className="muted">
          {suggestionLabel(e.source)}：{e.suggestAllow ? "允许" : "拒绝"}（仅供参考，放行只取决于你的点击）
        </p>
        {props.error !== "" && (
          <div className="alert" role="alert">
            <p>{props.error}</p>
          </div>
        )}
        <div className="actions">
          <button type="button" disabled={props.busy} onClick={props.onAllow}>
            本次运行允许
          </button>
          <button
            type="button"
            className="btn-danger btn-solid"
            disabled={props.busy}
            onClick={props.onDeny}
          >
            拒绝
          </button>
        </div>
      </div>
    </section>
  );
}
