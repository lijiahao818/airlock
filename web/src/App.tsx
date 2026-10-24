import { useState, type ReactNode } from "react";
import { useRoute } from "./router.ts";
import Wizard from "./pages/Wizard.tsx";
import Keys from "./pages/Keys.tsx";
import NewCabin from "./pages/NewCabin.tsx";

// 向导只需要走一次。完成标记放在浏览器本地，和密钥无关。
const wizardDoneKey = "airlock-wizard-done";

function wizardFinished(): boolean {
  return window.localStorage.getItem(wizardDoneKey) === "1";
}

export default function App() {
  const [done, setDone] = useState(wizardFinished());
  const [path, navigate] = useRoute();

  if (!done) {
    return (
      <Wizard
        onFinish={() => {
          window.localStorage.setItem(wizardDoneKey, "1");
          setDone(true);
        }}
      />
    );
  }

  let page: ReactNode;
  if (path === "/keys") {
    page = <Keys />;
  } else if (path === "/new") {
    page = <NewCabin onCreated={() => navigate("/")} />;
  } else {
    page = <p className="muted">工作舱列表建设中。</p>;
  }

  return (
    <>
      <header className="topbar">
        <span className="brand">Airlock</span>
        <nav>
          <button
            type="button"
            className={path === "/" ? "nav-on" : ""}
            onClick={() => navigate("/")}
          >
            工作舱
          </button>
          <button
            type="button"
            className={path === "/keys" ? "nav-on" : ""}
            onClick={() => navigate("/keys")}
          >
            密钥
          </button>
        </nav>
        <button
          type="button"
          className="btn-primary"
          onClick={() => navigate("/new")}
        >
          新建工作舱
        </button>
      </header>
      <main className="page">{page}</main>
    </>
  );
}
