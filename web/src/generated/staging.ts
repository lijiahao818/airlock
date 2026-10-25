// 本文件由 go run ./tools/tsgen 生成，不要手动修改。
// 来源：internal/staging

export type Kind = "added" | "modified" | "deleted";

export interface Change {
  path: string; // 斜杠分隔的相对路径
  kind: Kind;
  conflict: boolean; // 项目文件夹中的同一文件和开始运行时不一样
  special: boolean; // 符号链接、特殊文件，或 Windows 不能正确读写的文件名，不能写回项目
  applied: boolean; // applied.json 中记录这个改动已经写回。不是冲突，也不需要再写回
}
