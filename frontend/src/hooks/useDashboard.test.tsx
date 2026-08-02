import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DashboardClient } from "../client";
import type { GetDashboardResponse } from "../gen/dashboard/v1/dashboard_pb";
import { makeDashboard } from "../test/fixtures";
import { useDashboard } from "./useDashboard";

// Mock the client module so the default (no-arg) code path does not hit the
// network; explicit-client tests below ignore this mock entirely.
vi.mock("../client", () => ({
  createDashboardClient: vi.fn(() => ({
    getDashboard: vi.fn(() => Promise.resolve({ dashboard: undefined })),
  })),
}));

type GetDashboard = () => Promise<Partial<GetDashboardResponse>>;

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

afterEach(() => vi.clearAllMocks());

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
        return call === 1 ? Promise.resolve({ dashboard: d1 }) : Promise.reject(err);
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

  it("ignores a resolution that arrives after unmount", async () => {
    const d = deferred<Partial<GetDashboardResponse>>();
    const client = fakeClient(vi.fn(() => d.promise));

    const { result, unmount } = renderHook(() => useDashboard(client));
    unmount();

    await act(async () => {
      d.resolve({ dashboard: makeDashboard() });
      await d.promise;
    });
    // no state update happened after unmount
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

  it("falls back to the default client when none is provided", async () => {
    const { result } = renderHook(() => useDashboard());
    await waitFor(() => expect(result.current.status).toBe("success"));
  });
});
