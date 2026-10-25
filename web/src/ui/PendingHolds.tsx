import { useEffect, useState } from "react";
import { listCabins, listHolds } from "../api/console.ts";

// pollMs 是查询待处理暂扣的间隔。暂扣 60 秒后超时，所以间隔要远小于 60 秒。
const pollMs = 3000;

// PendingHolds 在导航栏提示其他工作舱正在等待用户决定的暂扣（#72）。
// 用户不在运行页时也能看到，点击后进入对应的运行页。current 是当前运行页的工作舱，它的暂扣已经显示在页面上。
export default function PendingHolds(props: { current: string; onOpen: (cabinId: string) => void }) {
  const [waiting, setWaiting] = useState<{ cabin: string; name: string; count: number }[]>([]);

  useEffect(() => {
    let alive = true;
    const poll = async () => {
      try {
        const cabins = (await listCabins()).filter((c) => c.status === "running");
        const found = [];
        for (const c of cabins) {
          const holds = await listHolds(c.id);
          if (holds.length > 0) {
            found.push({ cabin: c.id, name: c.name, count: holds.length });
          }
        }
        if (alive) {
          setWaiting(found);
        }
      } catch {
        // 查询失败时保留上一次的结果，下一次轮询再试。
      }
    };
    void poll();
    const timer = setInterval(() => void poll(), pollMs);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, []);

  const others = waiting.filter((w) => w.cabin !== props.current);
  if (others.length === 0) {
    return null;
  }
  const first = others[0];
  const total = others.reduce((n, w) => n + w.count, 0);
  return (
    <button type="button" className="btn-danger" onClick={() => props.onOpen(first.cabin)}>
      「{first.name}」等你处理 {total} 个拦截
    </button>
  );
}
