import { useEffect, useState, type ReactNode } from "react";
import { finishWizard, getWizard } from "./api/console.ts";
import { useRoute } from "./router.ts";
import PendingHolds from "./ui/PendingHolds.tsx";
import Icon from "./ui/Icon.tsx";
import Wizard from "./pages/Wizard.tsx";
import Keys from "./pages/Keys.tsx";
import NewCabin from "./pages/NewCabin.tsx";
import Cabins from "./pages/Cabins.tsx";
import Run from "./pages/Run.tsx";
import Review from "./pages/Review.tsx";

// cabinQuery 从 /run、/review 的查询参数里取工作舱编号。没有时返回空字符串。
function cabinQuery(query: URLSearchParams): string {
  return query.get("cabin") ?? "";
}

export default function App() {
  // 向导只需要走一次。完成标志保存在服务端的数据目录中：控制台每次启动的端口不同，
  // 浏览器的 localStorage 不能跨次保存（#86）。null 表示还在读取。
  const [done, setDone] = useState<boolean | null>(null);
  const [path, navigate] = useRoute();

  useEffect(() => {
    getWizard().then(
      (w) => setDone(w.done),
      () => setDone(false),
    );
  }, []);

  if (done === null) {
    return <p className="muted">正在打开 Airlock……</p>;
  }
  if (!done) {
    return (
      <Wizard
        onFinish={() => {
          // 记录失败时，下次打开 Airlock 会再显示一次向导，不影响这次使用。
          void finishWizard().catch(() => {});
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
    page = <NewCabin onCreated={() => navigate("/")} onGoKeys={() => navigate("/keys")} />;
  } else if (route === "/run" && cabinID !== "") {
    page = (
      <Run
        cabinId={cabinID}
        autoStart={query.get("start") === "1"}
        goBack={() => navigate("/")}
        goReview={() => navigate(`/review?cabin=${encodeURIComponent(cabinID)}`)}
      />
    );
  } else if (route === "/review" && cabinID !== "") {
    page = (
      <Review
        cabinId={cabinID}
        goBack={() => navigate("/")}
        goRun={() => navigate(`/run?cabin=${encodeURIComponent(cabinID)}`)}
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
        <span className="brand">
          <Icon name="shield" />
          Airlock
        </span>
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
        <PendingHolds
          current={route === "/run" ? cabinID : ""}
          onOpen={(id) => navigate(`/run?cabin=${encodeURIComponent(id)}`)}
        />
        <button type="button" className="btn-primary" onClick={() => navigate("/new")}>
          新建工作舱
        </button>
      </header>
      <main className={wide ? "page-run" : "page"}>{page}</main>
    </>
  );
}
