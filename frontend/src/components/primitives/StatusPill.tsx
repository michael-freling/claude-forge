/**
 * StatusPill renders a running/stopped status pill. Colour is never the sole
 * signal — a dot plus a text label always convey the state.
 */
export function StatusPill({
  running,
  label,
  title,
}: {
  running: boolean;
  label?: string;
  title?: string;
}) {
  const text = label ?? (running ? "running" : "stopped");
  return (
    <span
      className={"pill " + (running ? "pill-ok" : "pill-off")}
      title={title ?? text}
    >
      <span className="dot" aria-hidden="true" />
      <span className="lbl">{text}</span>
    </span>
  );
}
