// 控制台 HTTP API 的前端封装。路由见 docs/architecture.md 第 5.10 节。
// 数据类型全部从 src/generated/ 导入；接口的外壳类型由 tsgen 从 internal/console 生成。

import type * as advisor from "../generated/advisor";
import type * as audit from "../generated/audit";
import type * as cabin from "../generated/cabin";
import type * as presets from "../generated/presets";
import type * as staging from "../generated/staging";
import type {
  AllowReq,
  CreateReq,
  HostReq,
  PathReq,
  PathResp,
  ProviderInfo,
  SecretReq,
  URLResp,
  WizardResp,
} from "../generated/console";
import { request } from "./client.ts";

export function checkDocker(): Promise<void> {
  return request<void>("/api/docker");
}

export function listProviders(): Promise<ProviderInfo[]> {
  return request<ProviderInfo[]>("/api/providers");
}

export function setKey(provider: string, secret: string): Promise<void> {
  return request<void>(`/api/keys/${encodeURIComponent(provider)}`, {
    method: "PUT",
    body: JSON.stringify({ secret } satisfies SecretReq),
  });
}

export function deleteKey(provider: string): Promise<void> {
  return request<void>(`/api/keys/${encodeURIComponent(provider)}`, { method: "DELETE" });
}

export function testKey(provider: string): Promise<void> {
  return request<void>(`/api/keys/${encodeURIComponent(provider)}/test`, { method: "POST" });
}

export function listPresets(): Promise<presets.Preset[]> {
  return request<presets.Preset[]>("/api/presets");
}

export function listTiers(): Promise<string[]> {
  return request<string[]>("/api/tiers");
}

export function getAdvisor(): Promise<advisor.Settings> {
  return request<advisor.Settings>("/api/advisor");
}

export function setAdvisor(s: advisor.Settings): Promise<void> {
  return request<void>("/api/advisor", { method: "PUT", body: JSON.stringify(s) });
}

export function pickFolder(): Promise<PathResp> {
  return request<PathResp>("/api/pick-folder", { method: "POST" });
}

export function listCabins(): Promise<cabin.Cabin[]> {
  return request<cabin.Cabin[]>("/api/cabins");
}

export function createCabin(req: CreateReq): Promise<cabin.Cabin> {
  return request<cabin.Cabin>("/api/cabins", { method: "POST", body: JSON.stringify(req) });
}

export function startCabin(id: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/start`, { method: "POST" });
}

export function stopCabin(id: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/stop`, { method: "POST" });
}

export function resetCabin(id: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/reset`, { method: "POST" });
}

export function getAgentURL(id: string): Promise<URLResp> {
  return request<URLResp>(`/api/cabins/${encodeURIComponent(id)}/agent-url`);
}

export function openTerminal(id: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/terminal`, { method: "POST" });
}

export function entryFindings(id: string): Promise<cabin.Finding[]> {
  return request<cabin.Finding[]>(`/api/cabins/${encodeURIComponent(id)}/entry`);
}

export function reviewItems(id: string): Promise<cabin.ReviewItem[]> {
  return request<cabin.ReviewItem[]>(`/api/cabins/${encodeURIComponent(id)}/changes`);
}

export function getDiff(id: string, path: string): Promise<staging.Diff> {
  return request<staging.Diff>(
    `/api/cabins/${encodeURIComponent(id)}/diff?path=${encodeURIComponent(path)}`,
  );
}

export function revertChange(id: string, path: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/revert`, {
    method: "POST",
    body: JSON.stringify({ path } satisfies PathReq),
  });
}

export function applyChange(id: string, path: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/apply`, {
    method: "POST",
    body: JSON.stringify({ path } satisfies PathReq),
  });
}

export function finishReview(id: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/finish`, { method: "POST" });
}

export function getReport(id: string): Promise<audit.Report> {
  return request<audit.Report>(`/api/cabins/${encodeURIComponent(id)}/report`);
}

export function listHolds(id: string): Promise<cabin.Hold[]> {
  return request<cabin.Hold[]>(`/api/cabins/${encodeURIComponent(id)}/holds`);
}

export function getWizard(): Promise<WizardResp> {
  return request<WizardResp>("/api/wizard");
}

export function finishWizard(): Promise<void> {
  return request<void>("/api/wizard", { method: "POST" });
}

export function decideHold(holdID: string, allow: boolean): Promise<void> {
  return request<void>(`/api/holds/${encodeURIComponent(holdID)}`, {
    method: "POST",
    body: JSON.stringify({ allow } satisfies AllowReq),
  });
}

export function resetDecision(id: string, host: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/reset-decision`, {
    method: "POST",
    body: JSON.stringify({ host } satisfies HostReq),
  });
}
