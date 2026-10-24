import { useEffect, useState } from "react";
import type * as presets from "../generated/presets";
import { createCabin, listPresets, listTiers, pickFolder } from "../api/console.ts";
import { errText } from "../api/client.ts";
import { providerName, tierInfo } from "../ui/names.ts";

export default function NewCabin({ onCreated }: { onCreated: () => void }) {
  const [allPresets, setAllPresets] = useState<presets.Preset[]>([]);
  const [tiers, setTiers] = useState<string[]>([]);
  const [project, setProject] = useState("");
  const [presetID, setPresetID] = useState("");
  const [tier, setTier] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    Promise.all([listPresets(), listTiers()])
      .then(([ps, ts]) => {
        setAllPresets(ps);
        setTiers(ts);
        // 新建的工作舱默认用标准档（product.md 的 F-4）。
        setTier(ts.includes("standard") ? "standard" : (ts[0] ?? ""));
      })
      .catch((err) => setError(errText(err)));
  }, []);

  async function chooseFolder() {
    setError("");
    setBusy(true);
    try {
      const { path } = await pickFolder();
      // 用户取消选择时后端返回空路径，保持原来的选择不变。
      if (path !== "") {
        setProject(path);
      }
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy(false);
    }
  }

  async function submit() {
    setError("");
    setBusy(true);
    try {
      await createCabin({ project, preset: presetID, tier });
      onCreated();
    } catch (err) {
      setError(errText(err));
      setBusy(false);
    }
  }

  const ready = project !== "" && presetID !== "" && tier !== "";

  return (
    <>
      <h1>新建工作舱</h1>

      {error !== "" && (
        <div className="alert" role="alert">
          <p>{error}</p>
        </div>
      )}

      <section className="card">
        <h2>1 · 选择项目文件夹</h2>
        <p className="muted">
          工作舱是 AI 助手干活的地方。它改动的文件先留在暂存区，运行结束、你审查后才写回这个文件夹。
        </p>
        <div className="row">
          <button type="button" disabled={busy} onClick={() => void chooseFolder()}>
            选择文件夹
          </button>
          {project !== "" && <span className="cabin-project">{project}</span>}
        </div>
      </section>

      <section className="card">
        <h2>2 · 选择 AI 助手</h2>
        {allPresets.length === 0 && <p className="muted">正在读取可用的助手……</p>}
        <div className="radio-cards">
          {allPresets.map((p) => (
            <button
              type="button"
              key={p.id}
              className={"radio-card" + (presetID === p.id ? " on" : "")}
              onClick={() => setPresetID(p.id)}
            >
              {p.name}
              <span className="desc">
                {providerName(p.provider)} · {p.ui === "web" ? "网页界面" : "终端界面"}
              </span>
            </button>
          ))}
        </div>
      </section>

      <section className="card">
        <h2>3 · 选择安全档位</h2>
        <p className="muted">
          档位决定哪些出网请求自动放行。档位之外的请求一律先暂扣，由你决定。
        </p>
        <div className="radio-cards">
          {tiers.map((t) => (
            <button
              type="button"
              key={t}
              className={"radio-card" + (tier === t ? " on" : "")}
              onClick={() => setTier(t)}
            >
              {tierInfo(t).name}
              <span className="desc">{tierInfo(t).desc}</span>
            </button>
          ))}
        </div>
      </section>

      <div className="actions">
        <button
          type="button"
          className="btn-primary"
          disabled={!ready || busy}
          onClick={() => void submit()}
        >
          创建工作舱
        </button>
        {!ready && <span className="muted">选好上面三项后才能创建。</span>}
      </div>
    </>
  );
}
