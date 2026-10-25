// 本文件由 go run ./tools/tsgen 生成，不要手动修改。
// 来源：internal/staging

// Op 是差异中一行的类型。
export type Op = "add" | "del" | "same" | "skip";

export interface Line {
  op: Op;
  text: string; // 不含换行符。Op 为 skip 时为空
  skip?: number; // Op 为 skip 时省略的行数
}

// Diff 是一个改动的内容差异。内容来自沙箱，属于不可信数据，只能当纯文本显示（红线 6）。
export interface Diff {
  lines: Line[];
  reason?: string; // 不显示内容的原因，给用户看的大白话。不为空时 Lines 为空
}

export type Kind = "added" | "modified" | "deleted";

export interface Change {
  path: string; // 斜杠分隔的相对路径
  kind: Kind;
  conflict: boolean; // 项目文件夹中的同一文件和开始运行时不一样
  special: boolean; // 符号链接、特殊文件，或 Windows 不能正确读写的文件名，不能写回项目
  applied: boolean; // applied.json 中记录这个改动已经写回。不是冲突，也不需要再写回
}
