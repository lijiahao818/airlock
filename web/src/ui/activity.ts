import type * as audit from "../generated/audit";

// HostAct 是出网动态里一个网站的累计记录。
export interface HostAct {
  host: string;
  allowed: number;
  denied: number;
  holds: number; // 暂扣过几次
  // 最后一次决定命中的规则：user-allow、user-deny、run-allow、run-deny 或 timeout。空表示没有决定。
  // 超时不在闸门中留下决定，所以 timeout 不能撤销。
  decision: string;
}

// activityFromReport 用本次运行的报告重建出网动态。运行页打开或刷新时，事件流不回放之前的事件，
// 所以先用报告补上已有的记录（#72）。
export function activityFromReport(rep: audit.Report): HostAct[] {
  const list: HostAct[] = rep.hosts.map((h) => ({
    host: h.host,
    allowed: h.allowed,
    denied: h.denied,
    holds: 0,
    decision: "",
  }));
  for (const h of rep.holds) {
    const a = list.find((x) => x.host === h.host);
    if (a) {
      a.holds += 1;
      a.decision = h.rule;
    }
  }
  return list;
}
