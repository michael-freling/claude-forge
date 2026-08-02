/**
 * ErrorState is the full-panel error shown when the very first load fails (no
 * data to fall back to), offering a retry.
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
        ⚠️
      </div>
      <h2>Couldn&rsquo;t load the dashboard</h2>
      <p className="muted">{error ? error.message : "Unknown error"}</p>
      <button className="btn btn-primary" onClick={onRetry}>
        Try again
      </button>
    </div>
  );
}
