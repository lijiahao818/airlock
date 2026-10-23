// 本文件由 go run ./tools/tsgen 生成，不要手动修改。
// 来源：internal/cabin

import type * as gate from "./gate";
import type * as staging from "./staging";

export type Status = "idle" | "running" | "review";

export interface Cabin {
  id: string;
  name: string; // 项目文件夹的名称
  project: string;
  preset: string;
  tier: string;
  status: Status;
  runId?: string; // 当前或最近一次运行
}

// HoldMeta 是安全副驾可以看到的全部数据。
export interface HoldMeta {
  agent: string;
  host: string;
  port: number;
  method: string;
  path?: string;
  rule: string;
}

export interface Explanation {
  what: string; // 发生了什么
  risk: string; // 风险多大
  riskLevel: string; // low、medium 或 high
  action: string; // 用户可以怎么做
  suggestAllow: boolean; // 只用于显示，不影响判定
  source: string; // template 或 ai
  aiUnavailable: boolean; // 安全副驾已开启，但这次没有生成 AI 解释
  promptVersion?: string; // Source 为 ai 时，生成解释的提示词版本
}

export interface Hold {
  id: string;
  cabin: string;
  runId: string;
  agent: string;
  host: string;
  port: number;
  method: string;
  path?: string;
  rule: string;
  explanation: Explanation;
}

export type FindingKind = "excluded" | "cached" | "notice" | "high-risk";

export interface Finding {
  path: string;
  kind: FindingKind;
  reason: string; // 给用户看的大白话
}

export interface ReviewItem {
  path: string; // 斜杠分隔的相对路径
  kind: staging.Kind;
  conflict: boolean; // 运行期间，项目文件夹中的同一文件也有改动
  special: boolean; // 符号链接或特殊文件，不能写回项目
  findings: Finding[];
}

export interface Event {
  kind: string; // request、hold、explained、hold-closed、status 或 progress
  cabin: string;
  request?: gate.Event; // Kind 为 request
  hold?: Hold; // Kind 为 hold、explained 或 hold-closed
  status?: Status; // Kind 为 status
  progress?: Progress; // Kind 为 progress
}

// Progress 是启动时复制项目文件的进度。
export interface Progress {
  done: number;
  total: number;
}
