import { useState } from "react";
import { DismissableBanner } from "./primitives/DismissableBanner";

/**
 * ErrorBanner surfaces a refresh failure as a dismissable red alert shown over
 * the (stale) content that is still on screen. The caller keys the element on
 * the error message so a new failure reappears after a dismissal.
 */
export function ErrorBanner({ error }: { error: Error }) {
  const [dismissed, setDismissed] = useState(false);
  if (dismissed) {
    return null;
  }
  return (
    <DismissableBanner
      tone="error"
      title="Refresh failed"
      dismissLabel="Dismiss error"
      onDismiss={() => setDismissed(true)}
    >
      <div className="muted" style={{ marginTop: 2 }}>
        {error.message}
      </div>
    </DismissableBanner>
  );
}
