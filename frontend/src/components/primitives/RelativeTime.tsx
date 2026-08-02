import { useEffect, useState } from "react";
import { fullTimestamp, relativeTime } from "../../lib/format";

/**
 * RelativeTime renders a live relative label (e.g. "5m ago") with an absolute
 * timestamp tooltip, refreshing itself every 30s. Undefined/invalid values fall
 * back to `empty` ("—" by default). A `title` override replaces the default
 * absolute-timestamp tooltip (used to show created + last-active together).
 */
export function RelativeTime({
  value,
  empty = "—",
  title,
}: {
  value?: Date;
  empty?: string;
  title?: string;
}) {
  const [, setTick] = useState(0);
  const valid = value != null && !Number.isNaN(value.getTime());
  // Depend on the instant, not the Date object: parents re-creating an equal
  // Date each render must not tear down and recreate the interval.
  const ms = valid ? value.getTime() : undefined;

  useEffect(() => {
    if (ms === undefined) {
      return;
    }
    const id = setInterval(() => setTick((t) => t + 1), 30000);
    return () => clearInterval(id);
  }, [ms]);

  if (!valid) {
    return <span className="time">{empty}</span>;
  }

  return (
    <time
      className="time"
      title={title ?? fullTimestamp(value)}
      dateTime={value.toISOString()}
    >
      {relativeTime(value)}
    </time>
  );
}
