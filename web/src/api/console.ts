// 控制台 HTTP API 的前端封装。路由见 docs/architecture.md 第 5.10 节。
// 数据类型全部从 src/generated/ 导入；接口的外壳类型由 tsgen 从 internal/console 生成。

import type * as advisor from "../generated/advisor";
import type * as cabin from "../generated/cabin";
import type * as presets from "../generated/presets";
import type { CreateReq, PathResp, ProviderInfo, SecretReq } from "../generated/console";
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
