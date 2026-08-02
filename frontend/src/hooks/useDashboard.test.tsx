import { act, renderHook, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DashboardClient } from "../client";
import type { GetDashboardResponse } from "../gen/dashboard/v1/dashboard_pb";
import { makeDashboard } from "../test/fixtures";
import { POLL_INTERVAL_MS, useDashboard } from "./useDashboard";

// Mock the client module so the default (no-arg) code path does not hit the
// network; explicit-client tests below ignore this mock entirely.
vi.mock("../client", () => ({
  createDashboardClient: vi.fn(() => ({
    getDashboard: vi.fn(() => Promise.resolve({ dashboard: undefined })),
  })),
}));

type GetDashboard = (
  req: object,
  opts?: { signal?: AbortSignal },
) => Promise<Partial<GetDashboardResponse>>;

function fakeClient(getDashboard: GetDashboard): DashboardClient {
  return { getDashboard } as unknown as DashboardClient;
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function setHidden(hidden: boolean) {
  Object.defineProperty(document, "hidden", {
    value: hidden,
    configurable: true,
  });
}

afterEach(() => {
  vi.clearAllMocks();
  vi.useRealTimers();
  Reflect.deleteProperty(document, "hidden");
});

describe("useDashboard", () => {
  it("loads on mount and reports success", async () => {
    const dashboard = makeDashboard({ warnings: ["hi"] });
    const client = fakeClient(vi.fn(() => Promise.resolve({ dashboard })));

    const { result } = renderHook(() => useDashboard(client));

    await waitFor(() => expect(result.current.status).toBe("success"));
    expect(result.current.data).toBe(dashboard);
    expect(result.current.error).toBeUndefined();
  });

  it("keeps prior data and surfaces the error when a refresh fails", async () => {
    const d1 = makeDashboard();
    const err = new Error("boom");
    let call = 0;
    const client = fakeClient(
      vi.fn(() => {
        call += 1;
        return call === 1
          ? Promise.resolve({ dashboard: d1 })
          : Promise.reject(err);
      }),
    );

    const { result } = renderHook(() => useDashboard(client));
    await waitFor(() => expect(result.current.status).toBe("success"));

    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.status).toBe("error"));

    expect(result.current.error).toBe(err);
    expect(result.current.data).toBe(d1); // stale data preserved
  });

  it("wraps a non-Error rejection in an Error", async () => {
    const client = fakeClient(vi.fn(() => Promise.reject("nope")));

    const { result } = renderHook(() => useDashboard(client));

    await waitFor(() => expect(result.current.status).toBe("error"));
    expect(result.current.error).toBeInstanceOf(Error);
    expect(result.current.error?.message).toBe("nope");
  });

  it("replaces data on a successful refresh", async () => {
    const d1 = makeDashboard({ warnings: ["one"] });
    const d2 = makeDashboard({ warnings: ["two"] });
    let call = 0;
    const client = fakeClient(
      vi.fn(() => {
        call += 1;
        return Promise.resolve({ dashboard: call === 1 ? d1 : d2 });
      }),
    );

    const { result } = renderHook(() => useDashboard(client));
    await waitFor(() => expect(result.current.data).toBe(d1));

    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.data).toBe(d2));
  });

  it("is a no-op while a call is already in flight", async () => {
    const d = deferred<Partial<GetDashboardResponse>>();
    const gd = vi.fn(() => d.promise);
    const client = fakeClient(gd);

    const { result } = renderHook(() => useDashboard(client));
    // mount already issued call #1 (still pending)
    act(() => result.current.refresh());
    expect(gd).toHaveBeenCalledTimes(1);

    await act(async () => {
      d.resolve({ dashboard: makeDashboard() });
      await d.promise;
    });
    await waitFor(() => expect(result.current.status).toBe("success"));
  });

  it("aborts the in-flight call on unmount and ignores a late resolution", async () => {
    const d = deferred<Partial<GetDashboardResponse>>();
    let signal: AbortSignal | undefined;
    const client = fakeClient(
      vi.fn((_req, opts) => {
        signal = opts?.signal;
        return d.promise;
      }),
    );

    const { result, unmount } = renderHook(() => useDashboard(client));
    expect(signal?.aborted).toBe(false);
    unmount();
    expect(signal?.aborted).toBe(true);

    await act(async () => {
      d.resolve({ dashboard: makeDashboard() });
      await d.promise;
    });
    // no state update happened after the abort
    expect(result.current.status).toBe("loading");
  });

  it("ignores a rejection that arrives after unmount", async () => {
    const d = deferred<Partial<GetDashboardResponse>>();
    const client = fakeClient(vi.fn(() => d.promise));

    const { result, unmount } = renderHook(() => useDashboard(client));
    unmount();

    await act(async () => {
      d.reject(new Error("late"));
      await d.promise.catch(() => undefined);
    });
    expect(result.current.status).toBe("loading");
  });

  it("survives a StrictMode double-mount: the remount issues its own call", async () => {
    const gd = vi.fn(() =>
      Promise.resolve({ dashboard: makeDashboard({ warnings: ["strict"] }) }),
    );
    const { result } = renderHook(() => useDashboard(fakeClient(gd)), {
      wrapper: StrictMode,
    });

    await waitFor(() => expect(result.current.status).toBe("success"));
    // first mount's call was aborted by the StrictMode cleanup; the second ran
    expect(gd).toHaveBeenCalledTimes(2);
    expect(result.current.data?.warnings).toEqual(["strict"]);
  });

  it("polls every 30s while the tab is visible", async () => {
    vi.useFakeTimers();
    const gd = vi.fn(() => Promise.resolve({ dashboard: makeDashboard() }));
    const { unmount } = renderHook(() => useDashboard(fakeClient(gd)));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(gd).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(gd).toHaveBeenCalledTimes(2);

    unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 3);
    });
    expect(gd).toHaveBeenCalledTimes(2); // interval cleared on unmount
  });

  it("skips the poll while a call is in flight", async () => {
    vi.useFakeTimers();
    const d = deferred<Partial<GetDashboardResponse>>();
    const gd = vi.fn(() => d.promise);
    renderHook(() => useDashboard(fakeClient(gd)));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 2);
    });
    expect(gd).toHaveBeenCalledTimes(1); // still the mount call

    await act(async () => {
      d.resolve({ dashboard: makeDashboard() });
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(gd).toHaveBeenCalledTimes(2);
  });

  it("skips the poll while hidden and refreshes when visible again", async () => {
    vi.useFakeTimers();
    const gd = vi.fn(() => Promise.resolve({ dashboard: makeDashboard() }));
    renderHook(() => useDashboard(fakeClient(gd)));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(gd).toHaveBeenCalledTimes(1);

    setHidden(true);
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 2);
    });
    expect(gd).toHaveBeenCalledTimes(1); // hidden: no polling

    setHidden(false);
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(gd).toHaveBeenCalledTimes(2); // immediate refresh on return
  });

  it("falls back to the default client when none is provided", async () => {
    const { result } = renderHook(() => useDashboard());
    await waitFor(() => expect(result.current.status).toBe("success"));
  });
});
