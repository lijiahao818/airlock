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
  hosts: HostSummary[]; // agent 访问过的主机和端口
  holds: HoldSummary[]; // 暂扣过的主机和用户的决定
  keyUses: Record<string, number>; // 供应商 → 换上真实密钥的次数
  files: Review[]; // 改动的文件和用户的决定
}

// HostSummary 是一个主机的一个端口的统计。同一主机的不同端口风险不同，所以分开统计（#73）。
export interface HostSummary {
  host: string;
  port: number;
  allowed: number;
  denied: number;
}

export interface HoldSummary {
  host: string;
  port: number;
  rule: string; // user-allow、user-deny 或 timeout
}
