import { useCallback, useEffect, useRef, useState } from "react";
import { createDashboardClient, type DashboardClient } from "../client";
import type { Dashboard } from "../gen/dashboard/v1/dashboard_pb";

export type DashboardStatus = "idle" | "loading" | "success" | "error";

/** How often the dashboard re-fetches itself while the tab is visible. */
export const POLL_INTERVAL_MS = 30_000;

export interface UseDashboardResult {
  status: DashboardStatus;
  data?: Dashboard;
  error?: Error;
  refresh: () => void;
}

/**
 * useDashboard fetches the dashboard snapshot on mount, exposes a `refresh`
 * action, and auto-refreshes every {@link POLL_INTERVAL_MS} — skipping ticks
 * while a call is in flight or the document is hidden, and refreshing
 * immediately when the tab becomes visible again. The client is injectable
 * (default {@link createDashboardClient}). On a refresh failure the previous
 * `data` is preserved so the UI can surface an error banner over stale
 * content. Each call carries an AbortSignal that is aborted on unmount; an
 * aborted call never updates state.
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
  const abortRef = useRef<AbortController | null>(null);

  const refresh = useCallback(() => {
    if (inFlight.current) {
      return;
    }
    inFlight.current = true;
    const controller = new AbortController();
    abortRef.current = controller;
    setStatus("loading");
    active
      .getDashboard({}, { signal: controller.signal })
      .then((res) => {
        if (controller.signal.aborted) {
          return;
        }
        setData(res.dashboard);
        setError(undefined);
        setStatus("success");
      })
      .catch((e: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        setError(e instanceof Error ? e : new Error(String(e)));
        setStatus("error");
        // Keep the prior `data` so the UI can show an error over stale content.
      })
      .finally(() => {
        // Only the latest call owns the in-flight latch: a StrictMode-aborted
        // first call must not clear the latch of its replacement.
        if (abortRef.current === controller) {
          inFlight.current = false;
        }
      });
  }, [active]);

  useEffect(() => {
    refresh();
    return () => {
      abortRef.current?.abort();
      // The aborted call never updates state; release the latch immediately so
      // a StrictMode remount (or the next mount) can start its own call.
      inFlight.current = false;
    };
  }, [refresh]);

  useEffect(() => {
    const tick = () => {
      if (!document.hidden) {
        refresh();
      }
    };
    const id = setInterval(tick, POLL_INTERVAL_MS);
    document.addEventListener("visibilitychange", tick);
    return () => {
      clearInterval(id);
      document.removeEventListener("visibilitychange", tick);
    };
  }, [refresh]);

  return { status, data, error, refresh };
}
