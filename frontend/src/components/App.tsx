import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { DashboardClient } from "../client";
import type { Dashboard } from "../gen/dashboard/v1/dashboard_pb";
import { useDashboard } from "../hooks/useDashboard";
import { ErrorBanner } from "./ErrorBanner";
import { ErrorState } from "./ErrorState";
import { Footer } from "./Footer";
import { GlobalPanel } from "./GlobalPanel";
import { Header } from "./Header";
import { LoadingState } from "./LoadingState";
import { NoProjectsState } from "./NoProjectsState";
import { ProjectCard } from "./ProjectCard";
import { SummaryStats } from "./SummaryStats";
import { WarningsBanner } from "./WarningsBanner";

/** App is the dashboard root: it wires the data hook to the layout. */
export function App({ client }: { client?: DashboardClient }) {
  const { status, data, error, refresh } = useDashboard(client);
  const loading = status === "loading";
  const generatedAt =
    data?.generatedAt != null ? timestampDate(data.generatedAt) : undefined;

  return (
    <>
      <Header generatedAt={generatedAt} loading={loading} onRefresh={refresh} />
      <main>
        {!data && (status === "loading" || status === "idle") ? (
          <LoadingState />
        ) : !data && status === "error" ? (
          <ErrorState error={error} onRetry={refresh} />
        ) : data ? (
          <DashboardContent
            data={data}
            error={status === "error" ? error : undefined}
          />
        ) : null}
      </main>
      <Footer />
    </>
  );
}

function DashboardContent({ data, error }: { data: Dashboard; error?: Error }) {
  const servers = data.global?.servers ?? [];
  const projects = data.projects;
  const warnings = data.warnings;
  const sessionCount = projects.reduce((n, p) => n + p.sessions.length, 0);
  const runningCount = projects.reduce(
    (n, p) => n + p.runningSessions.length,
    0,
  );

  return (
    <>
      {error && <ErrorBanner key={error.message} error={error} />}
      {warnings.length > 0 && <WarningsBanner warnings={warnings} />}
      <SummaryStats
        projectCount={projects.length}
        sessionCount={sessionCount}
        runningCount={runningCount}
        serverCount={servers.length}
      />
      <GlobalPanel servers={servers} />
      {projects.length ? (
        projects.map((p, i) => <ProjectCard key={p.id || i} project={p} />)
      ) : (
        <NoProjectsState />
      )}
    </>
  );
}
