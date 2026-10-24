import { useEffect, useState } from "react";

// useRoute 返回当前路径（含查询参数）和跳转函数。浏览器的前进后退会同步到路径上。
// 后端把未知的路径都回退到首页（internal/console/static.go），所以前端路由自己认路径。
// 运行页和审查页用查询参数携带工作舱 ID（?cabin=...），所以路径里必须保留查询参数。
export function useRoute(): [string, (to: string) => void] {
  const [path, setPath] = useState(window.location.pathname + window.location.search);
  useEffect(() => {
    const onPop = () => setPath(window.location.pathname + window.location.search);
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
  const navigate = (to: string) => {
    window.history.pushState(null, "", to);
    setPath(to);
  };
  return [path, navigate];
}
