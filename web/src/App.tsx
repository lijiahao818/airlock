import { useState, type ReactNode } from "react";
import { useRoute } from "./router.ts";
import Wizard from "./pages/Wizard.tsx";
import Keys from "./pages/Keys.tsx";
import NewCabin from "./pages/NewCabin.tsx";
import Cabins from "./pages/Cabins.tsx";
import Run from "./pages/Run.tsx";

// 向导只需要走一次。完成标志放在浏览器本地，和密钥无关。
const wizardDoneKey = "airlock-wizard-done";

function wizardFinished(): boolean {
  return window.localStorage.getItem(wizardDoneKey) === "1";
}

// cabinQuery 从 /run、/review 的查询参数里取工作舱编号。没有时返回空字符串。
function cabinQuery(query: URLSearchParams): string {
  return query.get("cabin") ?? "";
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

  // 查询参数不属于路由本身，用 ? 前面的部分匹配页面。
  const parts = path.split("?");
  const route = parts[0];
  const query = new URLSearchParams(parts[1] ?? "");
  const cabinID = cabinQuery(query);
  // 运行页要放助手界面和右侧栏，用宽布局；其他页面保持原来的宽度。
  const wide = route === "/run" && cabinID !== "";

  let page: ReactNode;
  if (route === "/keys") {
    page = <Keys />;
  } else if (route === "/new") {
    page = <NewCabin onCreated={() => navigate("/")} />;
  } else if (route === "/run" && cabinID !== "") {
    page = (
      <Run
        cabinId={cabinID}
        autoStart={query.get("start") === "1"}
        goBack={() => navigate("/")}
        goReview={() => navigate(`/review?cabin=${encodeURIComponent(cabinID)}`)}
      />
    );
  } else {
    page = (
      <Cabins
        onNew={() => navigate("/new")}
        onOpenRun={(id, start) =>
          navigate(`/run?cabin=${encodeURIComponent(id)}${start ? "&start=1" : ""}`)
        }
        onReview={(id) => navigate(`/review?cabin=${encodeURIComponent(id)}`)}
      />
    );
  }

  return (
    <>
      <header className="topbar">
        <span className="brand">Airlock</span>
        <nav>
          <button
            type="button"
            className={route === "/" ? "nav-on" : ""}
            onClick={() => navigate("/")}
          >
            工作舱
          </button>
          <button
            type="button"
            className={route === "/keys" ? "nav-on" : ""}
            onClick={() => navigate("/keys")}
          >
            密钥
          </button>
        </nav>
        <button type="button" className="btn-primary" onClick={() => navigate("/new")}>
          新建工作舱
        </button>
      </header>
      <main className={wide ? "page-run" : "page"}>{page}</main>
    </>
  );
}
