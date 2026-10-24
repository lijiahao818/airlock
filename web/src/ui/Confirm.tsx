import { type ReactNode } from "react";

// Confirm 是确认对话框。危险操作前用它向用户说明后果。
export default function Confirm(props: {
  title: string;
  children: ReactNode;
  confirmText: string;
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="dialog-backdrop">
      <div className="dialog" role="dialog" aria-modal="true">
        <h2>{props.title}</h2>
        {props.children}
        <div className="actions">
          <button type="button" onClick={props.onCancel}>
            取消
          </button>
          <button
            type="button"
            className={props.danger ? "btn-danger" : "btn-primary"}
            onClick={props.onConfirm}
          >
            {props.confirmText}
          </button>
        </div>
      </div>
    </div>
  );
}
