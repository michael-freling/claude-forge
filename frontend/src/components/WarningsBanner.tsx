import { useState } from "react";
import { DismissableBanner } from "./primitives/DismissableBanner";

/**
 * WarningsBanner surfaces the dashboard's non-fatal warnings as a dismissable
 * amber status note (`role="status"` — warnings are not errors). Dismissal is
 * keyed to the joined warnings text: a changed set of warnings reappears even
 * after an earlier set was dismissed.
 */
export function WarningsBanner({ warnings }: { warnings: string[] }) {
  const [dismissedFor, setDismissedFor] = useState<string | undefined>();
  const key = warnings.join("\n");
  if (!warnings.length || dismissedFor === key) {
    return null;
  }
  return (
    <DismissableBanner
      tone="warning"
      title={`Warning${warnings.length > 1 ? "s" : ""} (${warnings.length})`}
      dismissLabel="Dismiss warnings"
      onDismiss={() => setDismissedFor(key)}
    >
      <ul>
        {warnings.map((w, i) => (
          <li key={i}>{w}</li>
        ))}
      </ul>
    </DismissableBanner>
  );
}
