import { useState } from "react";

/**
 * WarningsBanner surfaces the dashboard's non-fatal warnings as a dismissable
 * alert. Rendering nothing once dismissed.
 */
export function WarningsBanner({ warnings }: { warnings: string[] }) {
  const [dismissed, setDismissed] = useState(false);
  if (dismissed || !warnings.length) {
    return null;
  }
  return (
    <div className="warn" role="alert">
      <span className="wi" aria-hidden="true">
        ⚠️
      </span>
      <div className="wbody">
        <strong>
          Warning{warnings.length > 1 ? "s" : ""} ({warnings.length})
        </strong>
        <ul>
          {warnings.map((w, i) => (
            <li key={i}>{w}</li>
          ))}
        </ul>
      </div>
      <button
        className="x"
        title="Dismiss"
        aria-label="Dismiss warnings"
        onClick={() => setDismissed(true)}
      >
        ×
      </button>
    </div>
  );
}
