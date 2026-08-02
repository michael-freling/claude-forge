import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DashboardClient } from "../client";
import type { GetDashboardResponse } from "../gen/dashboard/v1/dashboard_pb";
import { makeDashboard, makeProject, makeSession } from "../test/fixtures";
import { App } from "./App";

// The default (no-prop) path resolves through this mock instead of the network.
vi.mock("../client", () => ({
  createDashboardClient: vi.fn(() => ({
    getDashboard: vi.fn(() => Promise.resolve({ dashboard: undefined })),
  })),
}));

type Res = Partial<GetDashboardResponse>;

function fakeClient(getDashboard: () => Promise<Res>): DashboardClient {
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

const withProject = () =>
  makeDashboard({
    projects: [
      makeProject({ owner: "octo", repo: "cat", sessions: [makeSession()] }),
    ],
  });

afterEach(() => vi.clearAllMocks());

describe("App", () => {
  it("shows the loading state, then the dashboard on success", async () => {
    const d = deferred<Res>();
    render(<App client={fakeClient(() => d.promise)} />);

    expect(screen.getByText("Loading dashboard…")).toBeInTheDocument();

    await act(async () => {
      d.resolve({ dashboard: withProject() });
      await d.promise;
    });

    expect(
      await screen.findByRole("heading", { name: "octo/cat" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Loading dashboard…")).toBeNull();
  });

  it("renders NoProjectsState for an empty dashboard", async () => {
    const client = fakeClient(() =>
      Promise.resolve({ dashboard: makeDashboard() }),
    );
    render(<App client={client} />);
    expect(
      await screen.findByText("No claude-forge projects found."),
    ).toBeInTheDocument();
  });

  it("shows warnings and tolerates an absent global section", async () => {
    const client = fakeClient(() =>
      Promise.resolve({
        dashboard: makeDashboard({
          warnings: ["no GitHub token"],
          global: undefined,
          projects: [makeProject({ owner: "octo", repo: "cat", id: "" })],
        }),
      }),
    );
    render(<App client={client} />);

    expect(await screen.findByText("no GitHub token")).toBeInTheDocument();
    expect(screen.getByText("0 running")).toBeInTheDocument();
  });

  it("shows the ErrorState on first-load failure and recovers on retry", async () => {
    let call = 0;
    const client = fakeClient(() => {
      call += 1;
      return call === 1
        ? Promise.reject(new Error("offline"))
        : Promise.resolve({ dashboard: withProject() });
    });
    render(<App client={client} />);

    expect(
      await screen.findByText(/Couldn.t load the dashboard/),
    ).toBeInTheDocument();
    expect(screen.getByText("offline")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    expect(
      await screen.findByRole("heading", { name: "octo/cat" }),
    ).toBeInTheDocument();
  });

  it("replaces the data on a successful refresh", async () => {
    let call = 0;
    const client = fakeClient(() => {
      call += 1;
      const dashboard =
        call === 1
          ? makeDashboard({
              projects: [makeProject({ owner: "octo", repo: "one" })],
            })
          : makeDashboard({
              projects: [makeProject({ owner: "octo", repo: "two" })],
            });
      return Promise.resolve({ dashboard });
    });
    render(<App client={client} />);

    await screen.findByRole("heading", { name: "octo/one" });
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(
      await screen.findByRole("heading", { name: "octo/two" }),
    ).toBeInTheDocument();
  });

  it("keeps stale data and shows an ErrorBanner on refresh failure", async () => {
    let call = 0;
    const client = fakeClient(() => {
      call += 1;
      return call === 1
        ? Promise.resolve({ dashboard: withProject() })
        : Promise.reject(new Error("refresh boom"));
    });
    render(<App client={client} />);

    await screen.findByRole("heading", { name: "octo/cat" });
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

    await waitFor(() =>
      expect(screen.getByText("Refresh failed")).toBeInTheDocument(),
    );
    // stale content is still visible under the banner
    expect(
      screen.getByRole("heading", { name: "octo/cat" }),
    ).toBeInTheDocument();
    expect(screen.getByText("refresh boom")).toBeInTheDocument();
  });

  it("uses the default client when no client prop is passed", async () => {
    render(<App />);
    // The mocked default client resolves; the Header (Refresh button) mounts.
    expect(
      await screen.findByRole("button", { name: "Refresh" }),
    ).toBeInTheDocument();
  });
});
