import { useCallback, useEffect, useState } from "react";
import type * as advisor from "../generated/advisor";
import type { ProviderInfo } from "../generated/console";
import {
  deleteKey,
  getAdvisor,
  listProviders,
  setAdvisor,
  setKey,
  testKey,
} from "../api/console.ts";
import { errText } from "../api/client.ts";
import Confirm from "../ui/Confirm.tsx";
import AdvisorNotice from "../ui/AdvisorNotice.tsx";
import { effectiveProvider } from "../ui/advisor.ts";
import { providerName } from "../ui/names.ts";

// 副驾只支持这两家，见 product.md 的 F-11。
const advisorProviders = ["anthropic", "deepseek"];

const blurbs: Record<string, string> = {
  anthropic: "Claude 系列模型的密钥。密钥保存在系统钥匙串里，不进沙箱。",
  openai: "Codex 使用的密钥。密钥保存在系统钥匙串里，不进沙箱。",
  deepseek: "DeepSeek 模型的密钥。密钥保存在系统钥匙串里，不进沙箱。",
};

export default function Keys() {
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [settings, setSettings] = useState<advisor.Settings | null>(null);
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [deleting, setDeleting] = useState("");
  const [showNotice, setShowNotice] = useState(false);
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const [ps, st] = await Promise.all([listProviders(), getAdvisor()]);
      setProviders(ps);
      setSettings(st);
    } catch (err) {
      setError(errText(err));
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const hasKey = (name: string) =>
    (providers.find((p) => p.name === name)?.hint ?? "") !== "";
  const canEnableAdvisor = advisorProviders.some(hasKey);
  // 副驾实际使用的供应商。两家都有密钥、没有选择时是默认的 Anthropic（#95）。
  const using = effectiveProvider(settings?.provider ?? "", hasKey("anthropic"), hasKey("deepseek"));
  const usingDefault = advisorProviders.every(hasKey) && settings?.provider === "";

  async function applySettings(next: advisor.Settings, note: string) {
    setError("");
    setMessage("");
    setBusy(true);
    try {
      await setAdvisor(next);
      setSettings(next);
      setMessage(note);
    } catch (err) {
      setError(errText(err));
      await refresh();
    } finally {
      setBusy(false);
    }
  }

  // 首次开启前必须先给用户看数据发送说明（AC 见 #37）。
  function turnOnAdvisor() {
    if (!settings || !canEnableAdvisor) {
      return;
    }
    if (!settings.noticeShown) {
      setShowNotice(true);
      return;
    }
    void applySettings({ ...settings, enabled: true }, "安全副驾已开启。");
  }

  async function save(name: string) {
    const secret = (secrets[name] ?? "").trim();
    if (secret === "") {
      setError("密钥是空的。请粘贴完整的密钥后再保存。");
      return;
    }
    setError("");
    setMessage("");
    setBusy(true);
    try {
      await setKey(name, secret);
      setSecrets((prev) => ({ ...prev, [name]: "" }));
      setMessage(`${providerName(name)} 的密钥已保存到系统钥匙串。`);
      await refresh();
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy(false);
    }
  }

  async function test(name: string) {
    setError("");
    setMessage("");
    setBusy(true);
    try {
      await testKey(name);
      setMessage(`${providerName(name)} 的密钥可以正常使用。`);
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy(false);
    }
  }

  async function doDelete() {
    const name = deleting;
    setDeleting("");
    setError("");
    setMessage("");
    setBusy(true);
    try {
      await deleteKey(name);
      setMessage(`${providerName(name)} 的密钥已删除。`);
      await refresh();
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <h1>密钥管理</h1>

      {error !== "" && (
        <div className="alert" role="alert">
          <p>{error}</p>
        </div>
      )}
      {message !== "" && <p className="ok">{message}</p>}

      {providers.map((p) => (
        <section className="card" key={p.name}>
          <div className="row">
            <h2 className="grow">{providerName(p.name)}</h2>
            {p.hint !== "" && (
              <span className="badge badge-idle">已保存 ······{p.hint}</span>
            )}
          </div>
          <p className="muted">{blurbs[p.name] ?? ""}</p>
          <div className="row">
            <input
              type="password"
              autoComplete="off"
              placeholder={p.hint !== "" ? "输入新的密钥可以覆盖旧的" : "把密钥粘贴到这里"}
              value={secrets[p.name] ?? ""}
              onChange={(e) =>
                setSecrets((prev) => ({ ...prev, [p.name]: e.target.value }))
              }
            />
            <button type="button" disabled={busy} onClick={() => void save(p.name)}>
              保存
            </button>
            {p.hint !== "" && (
              <>
                <button type="button" disabled={busy} onClick={() => void test(p.name)}>
                  测试连接
                </button>
                <button
                  type="button"
                  className="btn-danger"
                  disabled={busy}
                  onClick={() => setDeleting(p.name)}
                >
                  删除
                </button>
              </>
            )}
          </div>
        </section>
      ))}

      {settings !== null && (
        <section className="card">
          <div className="row">
            <h2 className="grow">安全副驾</h2>
            <button
              type="button"
              disabled={busy || (!settings.enabled && !canEnableAdvisor)}
              className={settings.enabled ? "btn-danger" : "btn-primary"}
              onClick={() =>
                settings.enabled
                  ? void applySettings({ ...settings, enabled: false }, "安全副驾已关闭。拦截提示改用固定模板解释。")
                  : turnOnAdvisor()
              }
            >
              {settings.enabled ? "关闭" : "开启"}
            </button>
          </div>
          <p className="muted">
            {settings.enabled
              ? `开启中：拦截提示会请 ${providerName(using)} 的模型解释风险${usingDefault ? "（默认选择）" : ""}。`
              : canEnableAdvisor
                ? "关闭中：拦截提示只用固定模板解释。"
                : "关闭中：先保存 Anthropic 或 DeepSeek 的密钥，才能开启安全副驾。"}
          </p>
          {settings.enabled && advisorProviders.every(hasKey) && (
            <p className="row">
              <span>两家都有密钥，副驾使用：</span>
              {advisorProviders.map((n) => (
                <label key={n}>
                  <input
                    type="radio"
                    name="advisor-provider"
                    checked={using === n}
                    onChange={() =>
                      void applySettings(
                        { ...settings, provider: n },
                        `安全副驾改用 ${providerName(n)} 的模型。`,
                      )
                    }
                  />
                  {providerName(n)}
                  {usingDefault && n === "anthropic" ? "（默认）" : ""}
                </label>
              ))}
            </p>
          )}
        </section>
      )}

      {deleting !== "" && (
        <Confirm
          title={`删除 ${providerName(deleting)} 的密钥？`}
          danger
          confirmText="删除"
          onCancel={() => setDeleting("")}
          onConfirm={() => void doDelete()}
        >
          <p>密钥会从系统钥匙串里删掉。正在运行、用到这家密钥的工作舱会调用不了模型。</p>
          <p>删除后重新粘贴保存，就能恢复。</p>
        </Confirm>
      )}

      {showNotice && settings !== null && (
        <div className="dialog-backdrop">
          <div className="dialog">
            <AdvisorNotice />
            <div className="actions">
              <button type="button" onClick={() => setShowNotice(false)}>
                先不开
              </button>
              <button
                type="button"
                className="btn-primary"
                onClick={() => {
                  setShowNotice(false);
                  void applySettings(
                    { ...settings, enabled: true, noticeShown: true },
                    "安全副驾已开启。",
                  );
                }}
              >
                知道了，开启
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
