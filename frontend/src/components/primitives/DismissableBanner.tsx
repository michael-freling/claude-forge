import type { ReactNode } from "react";
import { WarningIcon } from "./icons";

/**
 * DismissableBanner is the shared shell of the warnings and error banners. The
 * `warning` tone renders amber with `role="status"` (non-fatal, polite); the
 * `error` tone renders red with `role="alert"` (assertive). Dismissal state
 * lives in the caller so it can decide when a banner comes back.
 */
export function DismissableBanner({
  tone,
  title,
  dismissLabel,
  onDismiss,
  children,
}: {
  tone: "warning" | "error";
  title: string;
  dismissLabel: string;
  onDismiss: () => void;
  children?: ReactNode;
}) {
  const error = tone === "error";
  return (
    <div
      className={"banner " + (error ? "banner-error" : "banner-warn")}
      role={error ? "alert" : "status"}
    >
      <span className="wi" aria-hidden="true">
        <WarningIcon size={17} />
      </span>
      <div className="wbody">
        <strong>{title}</strong>
        {children}
      </div>
      <button
        type="button"
        className="x"
        title="Dismiss"
        aria-label={dismissLabel}
        onClick={onDismiss}
      >
        ×
      </button>
    </div>
  );
}
