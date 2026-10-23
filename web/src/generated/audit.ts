// 本文件由 go run ./tools/tsgen 生成，不要手动修改。
// 来源：internal/audit

import type * as gate from "./gate";

export interface Entry {
  gate?: gate.Event; // 一次出网判定
  review?: Review; // 一个出舱审查决定
}

export interface Review {
  time: string;
  path: string;
  applied: boolean;
}

export interface Report {
  hosts: HostSummary[]; // agent 访问过的主机
  holds: HoldSummary[]; // 暂扣过的主机和用户的决定
  keyUses: Record<string, number>; // 供应商 → 换上真实密钥的次数
  files: Review[]; // 改动的文件和用户的决定
}

export interface HostSummary {
  host: string;
  allowed: number;
  denied: number;
}

export interface HoldSummary {
  host: string;
  rule: string; // user-allow、user-deny 或 timeout
}
