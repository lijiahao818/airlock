import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { loadToken, setToken } from "./api/token.ts";
import App from "./App.tsx";
import "./ui.css";

// 令牌在 URL 片段 #token= 中，不发给服务器。读到后立刻从地址栏里删掉。
const token = loadToken(window.location, {
  replace: (path) => window.history.replaceState(null, "", path),
});
setToken(token);

const root = createRoot(document.getElementById("root")!);

if (token === "") {
  root.render(
    <main className="page-narrow">
      <div className="card">
        <h1>缺少控制台令牌</h1>
        <p>这个页面只能从 Airlock 启动时自动打开的网址进入，网址里带有只属于你的令牌。</p>
        <p>请关掉这个页面，重新运行 airlock.exe，用自动打开的页面操作。</p>
      </div>
    </main>,
  );
} else {
  root.render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
