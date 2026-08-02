/** LoadingState is the full-panel spinner shown before the first snapshot. */
export function LoadingState() {
  return (
    <div className="state" role="status">
      <div className="spinner" aria-hidden="true" />
      <p className="muted">Loading dashboard…</p>
    </div>
  );
}
