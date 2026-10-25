import { useEffect, useRef, useState } from "react";
import type * as audit from "../generated/audit";
import type * as cabin from "../generated/cabin";
import { applyChange, finishReview, getReport, listCabins, reviewItems } from "../api/console.ts";
import { errText } from "../api/client.ts";
import { changeKindName, findingKindName, providerName, statusName } from "../ui/names.ts";

// holdDecisionText 把暂扣的决定规则转成大白话。规则取值见 docs/architecture.md 第 5.1 节。
function holdDecisionText(rule: string): string {
  if (rule === "user-allow") {
    return "你允许了";
  }
  if (rule === "user-deny") {
    return "你拒绝了";
  }
  if (rule === "timeout") {
    return "超时没有决定，Airlock 自动拒绝";
  }
  return rule;
}

// 出舱审查页。默认应用全部改动（ADR-007），用户取消勾选就是丢弃；
// 完成审查后同页显示运行报告（issue #38 AC-8、AC-9）。
export default function Review(props: { cabinId: string; goBack: () => void; goRun: () => void }) {
  const cabinId = props.cabinId;
  const [info, setInfo] = useState<cabin.Cabin | null>(null);
  const [missing, setMissing] = useState(false);
  const [items, setItems] = useState<cabin.ReviewItem[] | null>(null);
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [itemErr, setItemErr] = useState<Record<string, string>>({});
  const [report, setReport] = useState<audit.Report | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    const init = async () => {
      try {
        const list = await listCabins();
        if (!alive.current) {
          return;
        }
        const c = list.find((x) => x.id === cabinId);
        if (!c) {
          setMissing(true);
          return;
        }
        setInfo(c);
        if (c.status === "review") {
          const its = await reviewItems(cabinId);
          if (!alive.current) {
            return;
          }
          setItems(its);
          const sel: Record<string, boolean> = {};
          for (const it of its) {
            // 默认应用全部改动。冲突和特殊文件不能写回，默认丢弃（AC-8）。已写回的改动保持勾选。
            sel[it.path] = it.applied || (!it.conflict && !it.special);
          }
          setSelected(sel);
        } else if (c.status === "idle") {
          // 上次运行已经审查完，直接看报告。重置过的工作舱没有报告。
          try {
            const rep = await getReport(cabinId);
            if (alive.current) {
              setReport(rep);
            }
          } catch {
            // 没有报告时停在提示页。
          }
        }
      } catch (err) {
        if (alive.current) {
          setError(errText(err));
        }
      }
    };
    void init();
    return () => {
      alive.current = false;
    };
  }, [cabinId]);

  // finish 应用勾选的改动并完成审查，然后显示运行报告。应用是一个一个做的，不能并发
  //（docs/architecture.md 第 5.7 节）。
  async function finish() {
    setError("");
    setBusy(true);
    try {
      const failed: Record<string, string> = {};
      for (const it of items ?? []) {
        if (!selected[it.path] || it.applied) {
          continue;
        }
        try {
          await applyChange(cabinId, it.path);
        } catch (err) {
          failed[it.path] = errText(err);
        }
      }
      setItemErr(failed);
      if (Object.keys(failed).length > 0) {
        setError("有改动没有应用成功。处理这些改动后再完成审查。");
        // 刷新改动列表，显示哪些改动已经写回。新发现的冲突改为丢弃，否则重试时还会失败。
        const its = await reviewItems(cabinId);
        if (alive.current) {
          setItems(its);
          setSelected((prev) => {
            const sel: Record<string, boolean> = {};
            for (const it of its) {
              sel[it.path] = it.applied || (!it.conflict && !it.special && prev[it.path] === true);
            }
            return sel;
          });
        }
        return;
      }
      await finishReview(cabinId);
      const rep = await getReport(cabinId);
      if (alive.current) {
        setReport(rep);
        // 审查完成后工作舱回到未运行，顶部的状态徽章跟着更新。
        setInfo((c) => (c === null ? c : { ...c, status: "idle" }));
      }
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy(false);
    }
  }

  if (missing) {
    return (
      <div className="card empty">
        <h1>找不到这个工作舱</h1>
        <p>它可能被删除了，或者网址里的工作舱编号不对。</p>
        <div className="actions">
          <button type="button" className="btn-primary" onClick={props.goBack}>
            回到工作舱列表
          </button>
        </div>
      </div>
    );
  }

  if (info === null) {
    return <p className="muted">正在读取工作舱……</p>;
  }

  const discarded = (items ?? []).filter((it) => !selected[it.path]).length;
  const anyApplied = (items ?? []).some((it) => it.applied);

  return (
    <>
      <div className="row">
        <button type="button" onClick={props.goBack}>
          ← 工作舱
        </button>
        <h1 className="grow">出舱审查</h1>
        <span className={`badge badge-${info.status}`}>{statusName(info.status)}</span>
      </div>
      {report === null && (
        <p className="muted">
          {anyApplied
            ? `「${info.name}」有一部分改动已经写回你的项目文件夹，其余改动还在下面的暂存区里。`
            : `「${info.name}」的改动都在下面的暂存区里，你的项目文件夹还没有被修改。`}
        </p>
      )}

      {error !== "" && (
        <div className="alert" role="alert">
          <p>{error}</p>
        </div>
      )}

      {report !== null && (
        <section className="card">
          <h2>运行报告</h2>
          <h3>访问的网站</h3>
          {report.hosts.length === 0 ? (
            <p className="muted">没有出网记录。</p>
          ) : (
            <ul className="plain-list">
              {report.hosts.map((h) => (
                <li key={h.host}>
                  <div className="row">
                    <code className="grow">{h.host}</code>
                    <span className="muted">
                      放行 {h.allowed} 次 · 拒绝 {h.denied} 次
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          )}
          <h3>暂扣和你的决定</h3>
          {report.holds.length === 0 ? (
            <p className="muted">没有请求被暂扣。</p>
          ) : (
            <ul className="plain-list">
              {report.holds.map((h, i) => (
                <li key={`${h.host}:${i}`}>
                  <div className="row">
                    <code className="grow">{h.host}</code>
                    <span>{holdDecisionText(h.rule)}</span>
                  </div>
                </li>
              ))}
            </ul>
          )}
          <h3>密钥使用</h3>
          {Object.keys(report.keyUses).length === 0 ? (
            <p className="muted">没有用到你的密钥。</p>
          ) : (
            <ul className="plain-list">
              {Object.entries(report.keyUses).map(([provider, n]) => (
                <li key={provider}>
                  <div className="row">
                    <span className="grow">{providerName(provider)}</span>
                    <span className="muted">密钥使用了 {n} 次</span>
                  </div>
                </li>
              ))}
            </ul>
          )}
          <h3>改动的文件</h3>
          {report.files.length === 0 ? (
            <p className="muted">没有改动。</p>
          ) : (
            <ul className="plain-list">
              {report.files.map((f) => (
                <li key={`${f.path}:${f.time}`}>
                  <div className="row">
                    <code className="grow">{f.path}</code>
                    <span className={f.applied ? "ok" : "muted"}>
                      {f.applied ? "已应用" : "已丢弃"}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          )}
          <div className="actions">
            <button type="button" className="btn-primary" onClick={props.goBack}>
              回到工作舱列表
            </button>
          </div>
        </section>
      )}

      {report === null && info.status === "running" && (
        <div className="card empty">
          <p>这个工作舱还在运行。先停止，改动才会进入出舱审查。</p>
          <div className="actions">
            <button type="button" className="btn-primary" onClick={props.goRun}>
              回到运行页
            </button>
          </div>
        </div>
      )}

      {report === null && info.status === "idle" && (
        <div className="card empty">
          <p>没有等待审查的改动。启动一次运行后，停止时才会进入出舱审查。</p>
          <div className="actions">
            <button type="button" className="btn-primary" onClick={props.goBack}>
              回到工作舱列表
            </button>
          </div>
        </div>
      )}

      {report === null && info.status === "review" && (
        <>
          <p>默认应用全部改动。不想应用的，取消勾选就是丢弃。</p>
          {discarded > 0 && (
            <div className="alert" role="alert">
              <p>你丢弃了 {discarded} 个改动。应用的改动可能不完整。</p>
            </div>
          )}
          {items === null && <p className="muted">正在读取改动……</p>}
          {items !== null && items.length === 0 && <p className="ok">本次运行没有改动。</p>}
          {items !== null && items.length > 0 && (
            <ul className="plain-list">
              {items.map((it) => {
                const highRisk = it.findings.some((f) => f.kind === "high-risk");
                const fixed = it.applied || it.conflict || it.special;
                return (
                  <li key={it.path} className={highRisk ? "change-danger" : ""}>
                    <label className="row check-row">
                      <input
                        type="checkbox"
                        checked={selected[it.path] === true}
                        disabled={fixed}
                        onChange={(ev) =>
                          setSelected((prev) => ({ ...prev, [it.path]: ev.target.checked }))
                        }
                      />
                      <span className={`badge change-${it.kind}`}>{changeKindName(it.kind)}</span>
                      <code className="grow">{it.path}</code>
                    </label>
                    {it.findings.map((f) => (
                      <p key={f.kind} className={f.kind === "high-risk" ? "danger-text" : "muted"}>
                        {findingKindName(f.kind)}：{f.reason}
                      </p>
                    ))}
                    {it.conflict && (
                      <p className="muted">
                        你的项目文件夹里也改了这个文件，不能自动应用。你的版本会保留，助手的版本丢弃。
                      </p>
                    )}
                    {it.applied && <p className="ok">已经写回你的项目文件夹。</p>}
                    {it.special && (
                      <p className="muted">
                        这是符号链接、特殊文件，或者文件名以点或空格结尾（Windows 不能正确读写），不能写回项目，将丢弃。
                      </p>
                    )}
                    {itemErr[it.path] !== undefined && (
                      <div className="alert" role="alert">
                        <p>{itemErr[it.path]}</p>
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
          <div className="actions">
            <button type="button" className="btn-primary" disabled={busy} onClick={() => void finish()}>
              应用并完成审查
            </button>
          </div>
          <p className="muted">
            完成审查后显示运行报告：访问了哪些网站、暂扣和决定、密钥使用、改动的文件。
          </p>
        </>
      )}
    </>
  );
}
