import { useState } from "react";
import type * as staging from "../generated/staging";
import { getDiff } from "../api/console.ts";
import { errText } from "../api/client.ts";

const signs: Record<staging.Op, string> = { add: "+ ", del: "- ", same: "  ", skip: "" };

// ChangeDiff 展开后显示一个改动的内容：你项目中现在的文件和助手的版本有哪些行不同（#98）。
// 内容来自沙箱，属于不可信数据。它只作为文本渲染，不用 innerHTML，网址也不变成链接（红线 6）。
export default function ChangeDiff(props: { cabinId: string; path: string }) {
  const [open, setOpen] = useState(false);
  const [diff, setDiff] = useState<staging.Diff | null>(null);
  const [error, setError] = useState("");

  async function toggle() {
    if (open) {
      setOpen(false);
      return;
    }
    setOpen(true);
    setDiff(null);
    setError("");
    try {
      setDiff(await getDiff(props.cabinId, props.path));
    } catch (err) {
      setError(errText(err));
    }
  }

  return (
    <div className="change-diff">
      <button type="button" onClick={() => void toggle()}>
        {open ? "收起内容" : "查看内容"}
      </button>
      {open && error !== "" && <p className="danger-text">读取内容失败：{error}</p>}
      {open && error === "" && diff === null && <p className="muted">正在读取……</p>}
      {open && diff !== null && diff.reason !== undefined && diff.reason !== "" && (
        <p className="muted">{diff.reason}</p>
      )}
      {open && diff !== null && !diff.reason && diff.lines.length === 0 && (
        <p className="muted">没有文字内容。</p>
      )}
      {open && diff !== null && !diff.reason && diff.lines.length > 0 && (
        <>
          <p className="muted">和你项目中现在的文件比较：“-”开头的行被助手删除，“+”开头的行是助手新增的。</p>
          <pre className="diff">
            {diff.lines.map((l, i) => (
              <div key={i} className={`diff-${l.op}`}>
                {l.op === "skip" ? `…… 省略 ${l.skip ?? 0} 行没有变化的内容` : signs[l.op] + l.text}
              </div>
            ))}
          </pre>
        </>
      )}
    </div>
  );
}
