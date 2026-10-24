import { useCallback, useEffect, useRef, useState } from "react";
import type * as cabin from "../generated/cabin";
import {
  listCabins,
  listPresets,
  resetCabin,
  stopCabin,
} from "../api/console.ts";
import { errText } from "../api/client.ts";
import Confirm from "../ui/Confirm.tsx";
import { statusName, tierInfo } from "../ui/names.ts";

export default function Cabins(props: {
  onNew: () => void;
  onOpenRun: (id: string, start: boolean) => void;
  onReview: (id: string) => void;
}) {
  const { onNew, onOpenRun, onReview } = props;
  const [cabins, setCabins] = useState<cabin.Cabin[] | null>(null);
  const [presetNames, setPresetNames] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [resetting, setResetting] = useState<cabin.Cabin | null>(null);
  const [busyID, setBusyID] = useState("");
  const alive = useRef(true);

  const refresh = useCallback(async () => {
    try {
      const list = await listCabins();
      if (alive.current) {
        setCabins(list);
        // 轮询成功说明网络恢复，清掉上一次轮询留下的错误提示。
        setError("");
      }
    } catch (err) {
      if (alive.current) {
        setError(errText(err));
      }
    }
  }, []);

  useEffect(() => {
    alive.current = true;
    listPresets()
      .then((ps) => {
        const m: Record<string, string> = {};
        for (const p of ps) {
          m[p.id] = p.name;
        }
        if (alive.current) {
          setPresetNames(m);
        }
      })
      .catch(() => {
        // 预设名取不到时，列表里直接显示预设 ID。
      });
    void refresh();
    // 状态会随运行变化，定时刷新列表。事件流驱动的运行页见 #38。
    const timer = setInterval(() => void refresh(), 3000);
    return () => {
      alive.current = false;
      clearInterval(timer);
    };
  }, [refresh]);

  async function act(c: cabin.Cabin, op: () => Promise<void>, note: string) {
    setError("");
    setMessage("");
    setBusyID(c.id);
    try {
      await op();
      setMessage(note);
      await refresh();
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusyID("");
    }
  }

  return (
    <>
      <h1>工作舱</h1>

      {error !== "" && (
        <div className="alert" role="alert">
          <p>{error}</p>
        </div>
      )}
      {message !== "" && <p className="ok">{message}</p>}

      {cabins === null && <p className="muted">正在读取工作舱……</p>}

      {cabins !== null && cabins.length === 0 && (
        <div className="card empty">
          <p>还没有工作舱。选好项目文件夹、AI 助手和安全档位，创建第一个。</p>
          <button type="button" className="btn-primary" onClick={onNew}>
            新建工作舱
          </button>
        </div>
      )}

      {cabins?.map((c) => (
        <section className="card" key={c.id}>
          <div className="row">
            <h2 className="grow">{c.name}</h2>
            <span className={`badge badge-${c.status}`}>{statusName(c.status)}</span>
          </div>
          <p className="cabin-project">{c.project}</p>
          <p className="muted">
            助手：{presetNames[c.preset] ?? c.preset} · 档位：{tierInfo(c.tier).name}
          </p>
          <div className="actions">
            {c.status === "idle" && (
              <button
                type="button"
                className="btn-primary"
                onClick={() => onOpenRun(c.id, true)}
              >
                启动
              </button>
            )}
            {c.status === "running" && (
              <>
                <button type="button" onClick={() => onOpenRun(c.id, false)}>
                  打开运行页
                </button>
                <button
                  type="button"
                  disabled={busyID === c.id}
                  onClick={() =>
                    void act(c, () => stopCabin(c.id), `「${c.name}」已停止，改动等待出舱审查。`)
                  }
                >
                  停止
                </button>
              </>
            )}
            {c.status === "review" && (
              <>
                <span className="muted">运行结束，改动等待出舱审查。</span>
                <button type="button" className="btn-primary" onClick={() => onReview(c.id)}>
                  去审查
                </button>
              </>
            )}
            <button
              type="button"
              className="btn-danger"
              disabled={busyID === c.id}
              onClick={() => setResetting(c)}
            >
              重置
            </button>
          </div>
        </section>
      ))}

      {resetting !== null && (
        <Confirm
          title={`重置「${resetting.name}」？`}
          danger
          confirmText="重置"
          onCancel={() => setResetting(null)}
          onConfirm={() => {
            const c = resetting;
            setResetting(null);
            void act(c, () => resetCabin(c.id), `「${c.name}」已重置。`);
          }}
        >
          <p>
            重置会丢弃沙箱和暂存区里的全部改动，并删除沙箱中安装的依赖。
            项目文件夹里已有的文件不受影响。
          </p>
          <p>重置后，工作舱回到“未运行”状态。</p>
        </Confirm>
      )}
    </>
  );
}
