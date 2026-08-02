/**
 * StatusPill renders a running/stopped status pill. Colour is never the sole
 * signal: the dot is hollow for stopped servers, and when a custom label (e.g.
 * a server name) replaces the state word, a visually-hidden state suffix keeps
 * the running/stopped distinction available to screen readers.
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
  const state = running ? "running" : "stopped";
  const text = label ?? state;
  return (
    <span
      className={"pill " + (running ? "pill-ok" : "pill-off")}
      title={title ?? text}
    >
      <span className="dot" aria-hidden="true" />
      <span className="lbl">{text}</span>
      {text !== state && <span className="sr-only">: {state}</span>}
    </span>
  );
}
