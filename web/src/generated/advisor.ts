// 本文件由 go run ./tools/tsgen 生成，不要手动修改。
// 来源：internal/advisor

export interface Settings {
  enabled: boolean; // 用户开启了安全副驾
  provider: string; // 用户添加了两家的密钥时选择的一家
  noticeShown: boolean; // 用户已经看过数据发送说明
}
