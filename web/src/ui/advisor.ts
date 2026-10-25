// 安全副驾在界面上的显示规则。

// effectiveProvider 返回副驾实际使用的供应商，与 advisor.selectProvider 一致。
// 两家都有密钥、用户没有选择时用 anthropic。没有可用的密钥时返回空字符串。
export function effectiveProvider(chosen: string, hasAnthropic: boolean, hasDeepseek: boolean): string {
  if (hasAnthropic && hasDeepseek) {
    return chosen === "deepseek" ? "deepseek" : "anthropic";
  }
  if (hasAnthropic) {
    return "anthropic";
  }
  return hasDeepseek ? "deepseek" : "";
}

// suggestionLabel 是拦截卡片上建议的标签。解释来自规则模板时，建议也来自固定规则，不是副驾给的。
export function suggestionLabel(source: string): string {
  return source === "ai" ? "副驾建议" : "规则的默认建议";
}
