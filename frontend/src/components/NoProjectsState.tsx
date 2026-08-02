/** NoProjectsState is shown when no claude-forge projects were found. */
export function NoProjectsState() {
  return (
    <section className="panel">
      <div className="state">
        <div className="ico" aria-hidden="true">
          📭
        </div>
        <h2>No claude-forge projects found.</h2>
        <p className="muted">
          Start a session with claude-forge and it will show up here.
        </p>
      </div>
    </section>
  );
}
