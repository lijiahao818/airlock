// 线条图标。路径是自己画的简单图形，不来自图标库。颜色跟随文字颜色。
const paths = {
  shield: "M12 3l8 3v6c0 4.6-3.4 8-8 9-4.6-1-8-4.4-8-9V6z M9 12l2 2 4-4",
  folder: "M3 7a1 1 0 0 1 1-1h5l2 2h9a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1z",
  bot: "M5 8h14v11H5z M9 13h.01 M15 13h.01 M12 4v4 M2 13v3 M22 13v3",
  alert: "M12 4l9 16H3z M12 10v4 M12 17h.01",
} as const;

export default function Icon(props: { name: keyof typeof paths }) {
  return (
    <svg
      className="icon"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={paths[props.name]} />
    </svg>
  );
}
