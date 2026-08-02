/** SummaryStats renders the row of summary chips at the top of the dashboard. */
export function SummaryStats({
  projectCount,
  sessionCount,
  runningCount,
  serverCount,
}: {
  projectCount: number;
  sessionCount: number;
  runningCount: number;
  serverCount: number;
}) {
  return (
    <div className="summary">
      <Stat n={projectCount} label={projectCount === 1 ? "project" : "projects"} />
      <Stat n={sessionCount} label={sessionCount === 1 ? "session" : "sessions"} />
      <Stat n={runningCount} label="running" />
      <Stat
        n={serverCount}
        label={serverCount === 1 ? "global server" : "global servers"}
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
