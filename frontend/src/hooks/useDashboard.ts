import { useCallback, useEffect, useRef, useState } from "react";
import { createDashboardClient, type DashboardClient } from "../client";
import type { Dashboard } from "../gen/dashboard/v1/dashboard_pb";

export type DashboardStatus = "idle" | "loading" | "success" | "error";

export interface UseDashboardResult {
  status: DashboardStatus;
  data?: Dashboard;
  error?: Error;
  refresh: () => void;
}

/**
 * useDashboard fetches the dashboard snapshot on mount and exposes a `refresh`
 * action. The client is injectable (default {@link createDashboardClient}). On a
 * refresh failure the previous `data` is preserved so the UI can surface an
 * error banner over stale content. A refresh is a no-op while a call is already
 * in flight.
 */
export function useDashboard(client?: DashboardClient): UseDashboardResult {
  const clientRef = useRef<DashboardClient | null>(null);
  if (clientRef.current === null) {
    clientRef.current = client ?? createDashboardClient();
  }
  const active = clientRef.current;

  const [status, setStatus] = useState<DashboardStatus>("idle");
  const [data, setData] = useState<Dashboard | undefined>(undefined);
  const [error, setError] = useState<Error | undefined>(undefined);

  const inFlight = useRef(false);
  const mounted = useRef(true);

  const refresh = useCallback(() => {
    if (inFlight.current) {
      return;
    }
    inFlight.current = true;
    setStatus("loading");
    active
      .getDashboard({})
      .then((res) => {
        if (!mounted.current) {
          return;
        }
        setData(res.dashboard);
        setError(undefined);
        setStatus("success");
      })
      .catch((e: unknown) => {
        if (!mounted.current) {
          return;
        }
        setError(e instanceof Error ? e : new Error(String(e)));
        setStatus("error");
        // Keep the prior `data` so the UI can show an error over stale content.
      })
      .finally(() => {
        inFlight.current = false;
      });
  }, [active]);

  useEffect(() => {
    mounted.current = true;
    refresh();
    return () => {
      mounted.current = false;
    };
  }, [refresh]);

  return { status, data, error, refresh };
}
