import { useState } from "react";

/**
 * ErrorBanner surfaces a refresh failure as a dismissable alert shown over the
 * (stale) content that is still on screen.
 */
export function ErrorBanner({ error }: { error: Error }) {
  const [dismissed, setDismissed] = useState(false);
  if (dismissed) {
    return null;
  }
  return (
    <div className="warn" role="alert">
      <span className="wi" aria-hidden="true">
        ⚠️
      </span>
      <div className="wbody">
        <strong>Refresh failed</strong>
        <div className="muted" style={{ marginTop: 2 }}>
          {error.message}
        </div>
      </div>
      <button
        className="x"
        title="Dismiss"
        aria-label="Dismiss error"
        onClick={() => setDismissed(true)}
      >
        ×
      </button>
    </div>
  );
}
