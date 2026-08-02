import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Navigate, Route, Routes } from "react-router-dom";
import type { DashboardClient } from "../client";
import { useDashboard } from "../hooks/useDashboard";
import { RunningPage } from "../pages/RunningPage";
import { ServersPage } from "../pages/ServersPage";
import { SessionsPage } from "../pages/SessionsPage";
import { ErrorBanner } from "./ErrorBanner";
import { ErrorState } from "./ErrorState";
import { Footer } from "./Footer";
import { Header } from "./Header";
import { LoadingState } from "./LoadingState";
import { WarningsBanner } from "./WarningsBanner";

/**
 * App is the dashboard shell: it fetches the snapshot once (shared by every
 * page), renders the chrome (header/nav, banners, loading/error states), and
 * routes between the Running (home), Sessions, and Servers pages. It expects
 * a Router in context (BrowserRouter in main, MemoryRouter in tests).
 */
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
          <>
            {status === "error" && error && (
              <ErrorBanner key={error.message} error={error} />
            )}
            {data.warnings.length > 0 && (
              <WarningsBanner warnings={data.warnings} />
            )}
            <Routes>
              <Route path="/" element={<RunningPage data={data} />} />
              <Route path="/sessions" element={<SessionsPage data={data} />} />
              <Route path="/servers" element={<ServersPage data={data} />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </>
        ) : null}
      </main>
      <Footer />
    </>
  );
}
