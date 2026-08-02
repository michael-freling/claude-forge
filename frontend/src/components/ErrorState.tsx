import { WarningIcon } from "./primitives/icons";

/**
 * ErrorState is the full-panel error shown when the very first load fails (no
 * data to fall back to). It leads with the likely cause — the dashboard
 * process not running — and keeps the raw error as secondary detail.
 */
export function ErrorState({
  error,
  onRetry,
}: {
  error?: Error;
  onRetry: () => void;
}) {
  return (
    <div className="state" role="alert">
      <div className="ico" aria-hidden="true">
        <WarningIcon size={34} />
      </div>
      <h2>Can&rsquo;t reach claude-forge</h2>
      <p className="muted">
        Is the <code className="mono">claude-forge dashboard</code> command
        still running?
      </p>
      <p className="err-detail mono">{error ? error.message : "Unknown error"}</p>
      <button type="button" className="btn btn-primary" onClick={onRetry}>
        Try again
      </button>
    </div>
  );
}
