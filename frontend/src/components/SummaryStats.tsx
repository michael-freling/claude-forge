/**
 * SummaryStats renders the quiet projects/sessions tally above the session
 * history. Plain text, deliberately not button-like: these are labels, not
 * actions.
 */
export function SummaryStats({
  projectCount,
  sessionCount,
}: {
  projectCount: number;
  sessionCount: number;
}) {
  return (
    <div className="summary">
      <Stat
        n={projectCount}
        label={projectCount === 1 ? "project" : "projects"}
      />
      <Stat
        n={sessionCount}
        label={sessionCount === 1 ? "session" : "sessions"}
      />
    </div>
  );
}

function Stat({ n, label }: { n: number; label: string }) {
  return (
    <div className="stat">
      <b>{n}</b>
      <span>{label}</span>
    </div>
  );
}
