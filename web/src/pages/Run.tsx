import { useCallback, useEffect, useRef, useState } from "react";
import type * as cabin from "../generated/cabin";
import type * as gate from "../generated/gate";
import {
  decideHold,
  entryFindings,
  getAgentURL,
  listCabins,
  listPresets,
  openTerminal,
  resetDecision,
  revertChange,
  reviewItems,
  startCabin,
  stopCabin,
} from "../api/console.ts";
import { errText } from "../api/client.ts";
import { followEvents } from "../api/events.ts";
import HoldCard from "../ui/HoldCard.tsx";
import Confirm from "../ui/Confirm.tsx";
import { changeKindName, findingKindName, ruleName, statusName } from "../ui/names.ts";

// HostAct 是出网动态里一个网站的累计记录。次数来自事件流，从打开页面时开始计数。
interface HostAct {
  host: string;
  allowed: number;
  denied: number;
  holds: number; // 暂扣过几次
  // 最后一次决定命中的规则：user-allow、user-deny、run-allow、run-deny 或 timeout。空表示没有决定。
  // 超时不在闸门中留下决定，所以 timeout 不能撤销。
  decision: string;
}

// applyRequest 把一次出网判定记进出网动态。决定类规则覆盖显示中的旧决定。
function applyRequest(list: HostAct[], e: gate.Event): HostAct[] {
  const idx = list.findIndex((a) => a.host === e.host);
  const cur: HostAct =
    idx >= 0 ? { ...list[idx] } : { host: e.host, allowed: 0, denied: 0, holds: 0, decision: "" };
  if (e.allowed) {
    cur.allowed += 1;
  } else {
    cur.denied += 1;
  }
  if (["user-allow", "user-deny", "run-allow", "run-deny", "timeout"].includes(e.rule)) {
    cur.decision = e.rule;
  }
  if (idx >= 0) {
    const copy = [...list];
    copy[idx] = cur;
    return copy;
  }
  return [...list, cur];
}

// bumpHolds 把一次暂扣记进出网动态的暂扣计数。
function bumpHolds(list: HostAct[], host: string): HostAct[] {
  const idx = list.findIndex((a) => a.host === host);
  if (idx < 0) {
    return [...list, { host, allowed: 0, denied: 0, holds: 1, decision: "" }];
  }
  const copy = [...list];
  copy[idx] = { ...copy[idx], holds: copy[idx].holds + 1 };
  return copy;
}

// 运行页。左边是 agent 的界面（独立来源的 iframe，见 ADR-006），右边是进舱检查、
// 拦截卡片、出网动态和运行中的改动。事件来自 fetch 读取的事件流，状态用轮询兜底
// （事件流不回放，慢消费者会丢事件）。
export default function Run(props: {
  cabinId: string;
  autoStart: boolean;
  goBack: () => void;
  goReview: () => void;
}) {
  const cabinId = props.cabinId;
  const [info, setInfo] = useState<cabin.Cabin | null>(null);
  const [missing, setMissing] = useState(false);
  const [agentURL, setAgentURL] = useState("");
  const [terminalUI, setTerminalUI] = useState(false);
  const [entry, setEntry] = useState<cabin.Finding[] | null>(null);
  const [progress, setProgress] = useState<cabin.Progress | null>(null);
  const [holds, setHolds] = useState<cabin.Hold[]>([]);
  const [holdErr, setHoldErr] = useState<{ id: string; text: string } | null>(null);
  const [activity, setActivity] = useState<HostAct[]>([]);
  const [changes, setChanges] = useState<cabin.ReviewItem[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [starting, setStarting] = useState(false);
  const [stopping, setStopping] = useState(false);

  const alive = useRef(true);
  const infoRef = useRef<cabin.Cabin | null>(null);
  const statusRef = useRef<cabin.Status>("idle");
  const runInfoRef = useRef(false); // 本次运行是否已取过 agent 地址等信息
  const entryLoadedRef = useRef(false); // 本次运行是否已取过进舱检查
  const wantStartRef = useRef(props.autoStart); // 从工作舱列表的“启动”按钮进来时要自动启动
  const startedRef = useRef(false); // 自动启动只触发一次
  const wentReviewRef = useRef(false); // 跳审查页只触发一次
  const goReviewRef = useRef(props.goReview);
  goReviewRef.current = props.goReview;

  // loadRunInfo 取只在进入运行态时需要的数据：agent 地址、预设类型、进舱检查。
  const loadRunInfo = useCallback(async () => {
    try {
      const u = await getAgentURL(cabinId);
      if (alive.current) {
        setAgentURL(u.url);
      }
    } catch (err) {
      // 运行刚结束时会取不到地址，此时左侧显示占位说明。
      if (alive.current && statusRef.current === "running") {
        setError(errText(err));
      }
    }
    try {
      const ps = await listPresets();
      if (alive.current) {
        const mine = ps.find((p) => p.id === infoRef.current?.preset);
        setTerminalUI(mine?.ui === "terminal");
      }
    } catch {
      // 预设取不到时，不显示“在终端窗口中打开”按钮。
    }
    try {
      const list = await entryFindings(cabinId);
      if (alive.current) {
        setEntry(list);
        entryLoadedRef.current = true;
      }
    } catch {
      // 进舱检查取不到时，卡片不显示。
    }
  }, [cabinId]);

  // applyStatus 是状态的唯一入口：事件流和轮询都走这里，避免两份数据打架。
  const applyStatus = useCallback(
    (next: cabin.Status) => {
      if (statusRef.current === next) {
        return;
      }
      statusRef.current = next;
      setInfo((c) => (c === null ? c : { ...c, status: next }));
      if (next === "running") {
        setStarting(false);
        if (!runInfoRef.current) {
          runInfoRef.current = true;
          setProgress(null);
          void loadRunInfo();
        }
      } else {
        runInfoRef.current = false;
        setAgentURL("");
        setChanges(null);
      }
      if (next === "review" && !wentReviewRef.current) {
        wentReviewRef.current = true;
        goReviewRef.current();
      }
    },
    [loadRunInfo],
  );

  useEffect(() => {
    alive.current = true;

    // 从地址栏中去掉 start=1。这样刷新页面时不会重复启动（issue #38 的启动跳转带它进来）。
    if (props.autoStart) {
      const q = new URLSearchParams(window.location.search);
      q.delete("start");
      const rest = q.toString();
      window.history.replaceState(
        null,
        "",
        window.location.pathname + (rest === "" ? "" : `?${rest}`),
      );
    }

    // 事件流：先订阅再启动，进度事件一个不丢（AC-2）。
    const ac = new AbortController();
    const onEvent = (e: cabin.Event) => {
      if (e.cabin !== cabinId) {
        return;
      }
      switch (e.kind) {
        case "progress":
          if (e.progress) {
            setProgress(e.progress);
            // 进舱检查在复制开始前就做完了，收到进度时一定查得到。
            if (!entryLoadedRef.current) {
              entryLoadedRef.current = true;
              void entryFindings(cabinId).then(
                (list) => {
                  if (alive.current) {
                    setEntry(list);
                  }
                },
                () => {},
              );
            }
          }
          break;
        case "status":
          if (e.status) {
            applyStatus(e.status);
          }
          break;
        case "hold":
          if (e.hold) {
            const h = e.hold;
            setHolds((prev) => (prev.some((x) => x.id === h.id) ? prev : [...prev, h]));
            setActivity((prev) => bumpHolds(prev, h.host));
          }
          break;
        case "explained":
          if (e.hold) {
            const h = e.hold;
            setHolds((prev) => prev.map((x) => (x.id === h.id ? h : x)));
          }
          break;
        case "hold-closed":
          if (e.hold) {
            const id = e.hold.id;
            setHolds((prev) => prev.filter((x) => x.id !== id));
            setHoldErr((prev) => (prev !== null && prev.id === id ? null : prev));
          }
          break;
        case "request":
          if (e.request) {
            const req = e.request;
            setActivity((prev) => applyRequest(prev, req));
          }
          break;
      }
    };
    followEvents(onEvent, ac.signal).catch((err) => {
      if (alive.current) {
        setError(errText(err));
      }
    });

    // 状态和改动用轮询兜底：事件流不回放，丢掉的事件靠下一次轮询补回来。
    const poll = async () => {
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
        infoRef.current = c;
        setInfo(c);
        applyStatus(c.status);
        if (wantStartRef.current) {
          // 自动启动只试一次：第一次轮询回来还是未运行才启动。
          wantStartRef.current = false;
          if (c.status === "idle" && !startedRef.current) {
            startedRef.current = true;
            void doStart();
          }
        }
        if (c.status === "running") {
          const items = await reviewItems(cabinId);
          if (alive.current) {
            setChanges(items);
          }
        }
      } catch (err) {
        if (alive.current) {
          setError(errText(err));
        }
      }
    };
    void poll();
    const timer = setInterval(() => void poll(), 3000);

    return () => {
      alive.current = false;
      ac.abort();
      clearInterval(timer);
    };
    // cabinId 变化时整个页面重新挂载；doStart 经由 refs 工作，不需要进依赖。
  }, [cabinId, applyStatus]);

  // doStart 启动一次运行。自动跳转和手动重试都走这里。
  async function doStart() {
    setError("");
    setStarting(true);
    setProgress(null);
    setHolds([]);
    setHoldErr(null);
    setActivity([]);
    setEntry(null);
    entryLoadedRef.current = false;
    try {
      // start 接口阻塞到沙箱建好才返回。复制进度从事先订阅的事件流来。
      await startCabin(cabinId);
      applyStatus("running");
    } catch (err) {
      setStarting(false);
      setError(errText(err));
    }
  }

  // decide 处理拦截卡片上的决定。卡片由 hold-closed 事件关闭。
  async function decide(h: cabin.Hold, allow: boolean) {
    setBusy(`hold:${h.id}`);
    setHoldErr(null);
    setError("");
    try {
      await decideHold(h.id, allow);
    } catch (err) {
      setHoldErr({ id: h.id, text: errText(err) });
    }
    setBusy("");
  }

  // undoDecision 撤销对某个网站的决定。之后助手再访问这个网站会再次暂扣。
  async function undoDecision(host: string) {
    setBusy(`undo:${host}`);
    setError("");
    try {
      await resetDecision(cabinId, host);
      setActivity((prev) => prev.map((a) => (a.host === host ? { ...a, decision: "" } : a)));
    } catch (err) {
      setError(errText(err));
    }
    setBusy("");
  }

  async function doRevert(path: string) {
    setBusy(`revert:${path}`);
    setError("");
    try {
      await revertChange(cabinId, path);
      const items = await reviewItems(cabinId);
      if (alive.current) {
        setChanges(items);
      }
    } catch (err) {
      setError(errText(err));
    }
    setBusy("");
  }

  async function doTerminal() {
    setBusy("term");
    setError("");
    try {
      await openTerminal(cabinId);
    } catch (err) {
      setError(errText(err));
    }
    setBusy("");
  }

  async function doStop() {
    setStopping(false);
    setBusy("stop");
    setError("");
    try {
      await stopCabin(cabinId);
      if (!wentReviewRef.current) {
        wentReviewRef.current = true;
        props.goReview();
      }
    } catch (err) {
      setError(errText(err));
      setBusy("");
    }
  }

  if (missing) {
    return (
      <main className="page">
        <div className="card empty">
          <h1>找不到这个工作舱</h1>
          <p>它可能被删除了，或者网址里的工作舱编号不对。</p>
          <div className="actions">
            <button type="button" className="btn-primary" onClick={props.goBack}>
              回到工作舱列表
            </button>
          </div>
        </div>
      </main>
    );
  }

  if (info === null) {
    return <p className="muted">正在读取工作舱……</p>;
  }

  const status = info.status;
  const holdBusy = (id: string) => busy === `hold:${id}`;

  return (
    <div className="run">
      <div className="row run-head">
        <button type="button" onClick={props.goBack}>
          ← 工作舱
        </button>
        <h1 className="grow">{info.name}</h1>
        <span className={`badge badge-${status}`}>{statusName(status)}</span>
        {status === "idle" && !starting && (
          <button
            type="button"
            className="btn-primary"
            disabled={busy !== ""}
            onClick={() => void doStart()}
          >
            启动
          </button>
        )}
        {status === "running" && terminalUI && (
          <button type="button" disabled={busy !== ""} onClick={() => void doTerminal()}>
            在终端窗口中打开
          </button>
        )}
        {status === "running" && (
          <button
            type="button"
            className="btn-danger"
            disabled={busy !== ""}
            onClick={() => setStopping(true)}
          >
            停止并审查
          </button>
        )}
      </div>

      {error !== "" && (
        <div className="alert" role="alert">
          <p>{error}</p>
        </div>
      )}

      <div className="run-body">
        <div className="run-left">
          {agentURL !== "" && status === "running" ? (
            <iframe className="run-agent" src={agentURL} title="助手界面" />
          ) : (
            <div className="run-agent-empty">
              <div className="card empty">
                {starting ? (
                  <p>正在启动：做进舱检查、复制项目文件、创建沙箱。第一次可能要几分钟。</p>
                ) : (
                  <p>助手没有在运行。点上面的“启动”开始。</p>
                )}
              </div>
            </div>
          )}
        </div>
        <div className="run-right">
          {starting && (
            <section className="card">
              <h3>正在启动</h3>
              {progress !== null ? (
                <>
                  <div
                    className="progressbar"
                    role="progressbar"
                    aria-valuemin={0}
                    aria-valuemax={progress.total}
                    aria-valuenow={progress.done}
                  >
                    <div
                      className="progressbar-fill"
                      style={{
                        width: progress.total === 0 ? "100%" : `${(progress.done / progress.total) * 100}%`,
                      }}
                    />
                  </div>
                  <p className="muted">正在复制项目文件（{progress.done}/{progress.total}）。</p>
                </>
              ) : (
                <p className="muted">正在做进舱检查……</p>
              )}
            </section>
          )}

          {entry !== null && (starting || entry.length > 0) && (
            <section className="card">
              <h3>进舱检查</h3>
              {entry.length === 0 ? (
                <p className="ok">没有发现需要注意的文件。</p>
              ) : (
                <ul className="plain-list">
                  {entry.map((f) => (
                    <li key={`${f.kind}:${f.path}`}>
                      <span className={`badge finding-${f.kind}`}>{findingKindName(f.kind)}</span>{" "}
                      <code>{f.path}</code>
                      <p className="muted">{f.reason}</p>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          )}

          {holds.map((h) => (
            <HoldCard
              key={h.id}
              hold={h}
              busy={holdBusy(h.id)}
              error={holdErr !== null && holdErr.id === h.id ? holdErr.text : ""}
              onAllow={() => void decide(h, true)}
              onDeny={() => void decide(h, false)}
            />
          ))}

          <section className="card">
            <h3>出网动态</h3>
            {activity.length === 0 ? (
              <p className="muted">还没有出网记录。</p>
            ) : (
              <ul className="plain-list">
                {activity.map((a) => (
                  <li key={a.host}>
                    <div className="row">
                      <code className="grow">{a.host}</code>
                      {a.decision !== "" && <span className="muted">{ruleName(a.decision)}</span>}
                    </div>
                    <div className="row">
                      <span className="muted">
                        放行 {a.allowed} 次 · 拒绝 {a.denied} 次
                        {a.holds > 0 ? ` · 暂扣 ${a.holds} 次` : ""}
                      </span>
                      {a.decision !== "" && a.decision !== "timeout" && status === "running" && (
                        <button
                          type="button"
                          disabled={busy !== ""}
                          onClick={() => void undoDecision(a.host)}
                        >
                          撤销决定
                        </button>
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            )}
            <p className="muted">撤销决定后，助手再次访问这个网站时会再问你一次。</p>
          </section>

          {status === "running" && (
            <section className="card">
              <h3>运行中的改动</h3>
              <p className="muted">
                改动先放在暂存区，不碰你的项目文件夹。停止后经出舱审查才写回。
              </p>
              {changes === null && <p className="muted">正在读取……</p>}
              {changes !== null && changes.length === 0 && <p className="ok">还没有改动。</p>}
              {changes !== null && changes.length > 0 && (
                <ul className="plain-list">
                  {changes.map((it) => {
                    const highRisk = it.findings.some((f) => f.kind === "high-risk");
                    return (
                      <li key={it.path} className={highRisk ? "change-danger" : ""}>
                        <div className="row">
                          <span className={`badge change-${it.kind}`}>{changeKindName(it.kind)}</span>
                          <code className="grow">{it.path}</code>
                        </div>
                        {it.findings.map((f) => (
                          <p key={f.kind} className={f.kind === "high-risk" ? "danger-text" : "muted"}>
                            {findingKindName(f.kind)}：{f.reason}
                          </p>
                        ))}
                        {it.conflict && (
                          <p className="muted">你的项目文件夹也改了这个文件，运行中不能撤销。</p>
                        )}
                        <div className="actions">
                          <button
                            type="button"
                            disabled={busy !== "" || it.conflict}
                            onClick={() => void doRevert(it.path)}
                          >
                            撤销这个改动
                          </button>
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
            </section>
          )}
        </div>
      </div>

      {stopping && info !== null && (
        <Confirm
          title={`停止「${info.name}」并进入出舱审查？`}
          confirmText="停止并审查"
          onCancel={() => setStopping(false)}
          onConfirm={() => void doStop()}
        >
          <p>停止会结束沙箱里的助手。改动保留在暂存区，等出舱审查时决定哪些写回项目。</p>
        </Confirm>
      )}
    </div>
  );
}
