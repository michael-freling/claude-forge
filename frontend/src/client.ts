import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { DashboardService } from "./gen/dashboard/v1/dashboard_pb";

export type DashboardClient = ReturnType<
  typeof createClient<typeof DashboardService>
>;

export const createDashboardClient = (baseUrl = "/"): DashboardClient =>
  createClient(DashboardService, createConnectTransport({ baseUrl }));
