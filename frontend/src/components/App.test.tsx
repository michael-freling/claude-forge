import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DashboardClient } from "../client";
import type { GetDashboardResponse } from "../gen/dashboard/v1/dashboard_pb";
import {
  makeDashboard,
  makeGlobal,
  makeProject,
  makeRunningSession,
  makeServer,
  makeSession,
} from "../test/fixtures";
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

function renderAt(route: string, client?: DashboardClient) {
  return render(
    <MemoryRouter initialEntries={[route]}>
      <App client={client} />
    </MemoryRouter>,
  );
}

const withProject = () =>
  makeDashboard({
    global: makeGlobal([makeServer({ name: "kubernetes" })]),
    projects: [
      makeProject({
        owner: "octo",
        repo: "cat",
        sessions: [makeSession({ name: "wire it" })],
        runningSessions: [makeRunningSession({ shortId: "abcdef12" })],
      }),
    ],
  });

afterEach(() => vi.clearAllMocks());

describe("App", () => {
  it("shows the loading state, then the Running home page on success", async () => {
    const d = deferred<Res>();
    renderAt("/", fakeClient(() => d.promise));

    expect(screen.getByText("Loading dashboard…")).toBeInTheDocument();

    await act(async () => {
      d.resolve({ dashboard: withProject() });
      await d.promise;
    });

    // the running session joined to its recorded name is the home content
    expect(
      await screen.findByRole("heading", { name: "wire it" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Loading dashboard…")).toBeNull();
    // no session-history table on the Running page
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("navigates between pages via the header tabs", async () => {
    renderAt(
      "/",
      fakeClient(() => Promise.resolve({ dashboard: withProject() })),
    );
    await screen.findByRole("heading", { name: "wire it" });

    fireEvent.click(screen.getByRole("link", { name: "Sessions" }));
    expect(
      await screen.findByRole("heading", { name: "octo/cat" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("table")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("link", { name: "Servers" }));
    expect(
      await screen.findByRole("heading", { name: "Global MCP servers" }),
    ).toBeInTheDocument();
  });

  it("deep-links directly to /sessions and /servers", async () => {
    const client = fakeClient(() =>
      Promise.resolve({ dashboard: withProject() }),
    );
    const { unmount } = renderAt("/sessions", client);
    expect(
      await screen.findByRole("heading", { name: "octo/cat" }),
    ).toBeInTheDocument();
    unmount();

    renderAt("/servers", client);
    expect(
      await screen.findByRole("heading", { name: "Global MCP servers" }),
    ).toBeInTheDocument();
  });

  it("redirects an unknown path to the Running home page", async () => {
    renderAt(
      "/nope",
      fakeClient(() => Promise.resolve({ dashboard: withProject() })),
    );
    expect(
      await screen.findByRole("heading", { name: "wire it" }),
    ).toBeInTheDocument();
  });

  it("renders the no-projects state on /sessions for an empty dashboard", async () => {
    renderAt(
      "/sessions",
      fakeClient(() => Promise.resolve({ dashboard: makeDashboard() })),
    );
    expect(
      await screen.findByText("No claude-forge projects found."),
    ).toBeInTheDocument();
  });

  it("shows warnings as a status note and tolerates an absent global section", async () => {
    renderAt(
      "/",
      fakeClient(() =>
        Promise.resolve({
          dashboard: makeDashboard({
            warnings: ["no GitHub token"],
            global: undefined,
            projects: [makeProject({ owner: "octo", repo: "cat", id: "" })],
          }),
        }),
      ),
    );

    expect(await screen.findByText("no GitHub token")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Warning (1)");
  });

  it("shows the ErrorState on first-load failure and recovers on retry", async () => {
    let call = 0;
    renderAt(
      "/",
      fakeClient(() => {
        call += 1;
        return call === 1
          ? Promise.reject(new Error("offline"))
          : Promise.resolve({ dashboard: withProject() });
      }),
    );

    expect(
      await screen.findByText(/Can.t reach claude-forge/),
    ).toBeInTheDocument();
    expect(screen.getByText("offline")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    expect(
      await screen.findByRole("heading", { name: "wire it" }),
    ).toBeInTheDocument();
  });

  it("replaces the data on a successful refresh", async () => {
    let call = 0;
    renderAt(
      "/sessions",
      fakeClient(() => {
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
      }),
    );

    await screen.findByRole("heading", { name: "octo/one" });
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(
      await screen.findByRole("heading", { name: "octo/two" }),
    ).toBeInTheDocument();
  });

  it("keeps stale data and shows an ErrorBanner on refresh failure", async () => {
    let call = 0;
    renderAt(
      "/",
      fakeClient(() => {
        call += 1;
        return call === 1
          ? Promise.resolve({ dashboard: withProject() })
          : Promise.reject(new Error("refresh boom"));
      }),
    );

    await screen.findByRole("heading", { name: "wire it" });
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

    await waitFor(() =>
      expect(screen.getByText("Refresh failed")).toBeInTheDocument(),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("refresh boom");
    // stale content is still visible under the banner
    expect(
      screen.getByRole("heading", { name: "wire it" }),
    ).toBeInTheDocument();
  });

  it("uses the default client when no client prop is passed", async () => {
    renderAt("/");
    // The mocked default client resolves; the Header (Refresh button) mounts.
    expect(
      await screen.findByRole("button", { name: "Refresh" }),
    ).toBeInTheDocument();
  });
});
