import type { ReactNode } from "react";

/** EmptyNote renders a muted inline note for empty lists/sections. */
export function EmptyNote({ children }: { children: ReactNode }) {
  return <div className="empty-note">{children}</div>;
}
