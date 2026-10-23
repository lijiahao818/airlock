// 本文件由 go run ./tools/tsgen 生成，不要手动修改。
// 来源：internal/staging

export type Kind = "added" | "modified" | "deleted";

export interface Change {
  path: string; // 斜杠分隔的相对路径
  kind: Kind;
  conflict: boolean; // 运行期间，项目文件夹中的同一文件也有改动
  special: boolean; // 符号链接或特殊文件，不能写回项目
}
