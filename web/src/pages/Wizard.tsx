import { useEffect, useState } from "react";
import { checkDocker } from "../api/console.ts";
import { errText } from "../api/client.ts";
import AdvisorNotice from "../ui/AdvisorNotice.tsx";

// DockerState 是 Docker 检查的三个阶段。
type DockerState =
  | { phase: "checking" }
  | { phase: "ok" }
  | { phase: "fail"; message: string };

export default function Wizard({ onFinish }: { onFinish: () => void }) {
  const [step, setStep] = useState(1);
  const [docker, setDocker] = useState<DockerState>({ phase: "checking" });

  const check = () => {
    setDocker({ phase: "checking" });
    checkDocker()
      .then(() => setDocker({ phase: "ok" }))
      .catch((err) => setDocker({ phase: "fail", message: errText(err) }));
  };

  useEffect(check, []);

  return (
    <main className="page-narrow">
      <h1>欢迎使用 Airlock</h1>
      <p className="muted">
        Airlock 把 AI 编程助手关进隔离的“工作舱”里运行：它改动的文件先留在暂存区，
        出网要经过你的同意，你的密钥永远不进沙箱。
      </p>

      {step === 1 && (
        <section className="card">
          <h2>第 1 步 · 检查 Docker</h2>
          {docker.phase === "checking" && <p>正在检查 Docker 是否已经就绪……</p>}
          {docker.phase === "ok" && (
            <p className="ok">Docker 已经就绪，可以继续。</p>
          )}
          {docker.phase === "fail" && (
            <div className="alert">
              <p>{docker.message}</p>
              <p>
                下一步：请安装 Docker Desktop，或者把它启动起来，然后点“重新检查”。
                Docker 就绪前无法启动工作舱，其他页面可以先用。
              </p>
            </div>
          )}
          <div className="actions">
            {docker.phase === "fail" && (
              <button type="button" onClick={check}>
                重新检查
              </button>
            )}
            {docker.phase === "ok" && (
              <button type="button" className="btn-primary" onClick={() => setStep(2)}>
                下一步
              </button>
            )}
          </div>
        </section>
      )}

      {step === 2 && (
        <section>
          <div className="card">
            <h2>第 2 步 · 了解安全副驾（可选）</h2>
            <p>
              安全副驾不是必须的。想开启的话，先在“密钥管理”页保存 Anthropic 或
              DeepSeek 的密钥，再打开开关。
            </p>
            <AdvisorNotice />
          </div>
          <div className="actions">
            <button type="button" onClick={() => setStep(1)}>
              上一步
            </button>
            <button type="button" className="btn-primary" onClick={() => setStep(3)}>
              下一步
            </button>
          </div>
        </section>
      )}

      {step === 3 && (
        <section className="card">
          <h2>第 3 步 · 一切就绪</h2>
          <p>
            去创建你的第一个工作舱吧：选择项目文件夹、AI 助手和安全档位，就可以开始干活。
          </p>
          <div className="actions">
            <button type="button" className="btn-primary" onClick={onFinish}>
              开始使用
            </button>
          </div>
        </section>
      )}
    </main>
  );
}
