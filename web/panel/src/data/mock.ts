export type RuntimeStatus = "healthy" | "warning" | "error";

export type SiteRecord = {
  id: string;
  name: string;
  upstream: string;
  domains: number;
  state: "active" | "pending" | "disabled";
  lastSync: string;
};

export type DomainRecord = {
  id: string;
  hostname: string;
  site: string;
  tls: "managed" | "pending" | "error";
  dns: "verified" | "waiting" | "mismatch";
  lastEvent: string;
};

export type JobRecord = {
  id: string;
  job: string;
  schedule: string;
  status: "idle" | "running" | "failed";
  lastRun: string;
};

export const runtimeCards = [
  {
    label: "Database",
    value: "Connected",
    status: "healthy" as const,
    detail: "SQLite file opened and writable",
  },
  {
    label: "Scheduler",
    value: "Idle",
    status: "warning" as const,
    detail: "Cron jobs are scaffolded but not registered yet",
  },
  {
    label: "Proxy runtime",
    value: "Skeleton mode",
    status: "warning" as const,
    detail: "Embedded Caddy manager is initialized but not serving :80/:443 yet",
  },
  {
    label: "Admin API",
    value: "Ready",
    status: "healthy" as const,
    detail: "Chi server is serving health and bootstrap endpoints",
  },
];

export const sites: SiteRecord[] = [
  {
    id: "site_1",
    name: "marketing-site",
    upstream: "http://127.0.0.1:3001",
    domains: 3,
    state: "active",
    lastSync: "2026-03-30T14:10:00Z",
  },
  {
    id: "site_2",
    name: "billing-api",
    upstream: "http://127.0.0.1:4100",
    domains: 1,
    state: "pending",
    lastSync: "2026-03-30T13:40:00Z",
  },
  {
    id: "site_3",
    name: "docs-panel",
    upstream: "http://127.0.0.1:3200",
    domains: 2,
    state: "disabled",
    lastSync: "2026-03-29T19:25:00Z",
  },
  {
    id: "site_4",
    name: "checkout-service",
    upstream: "http://127.0.0.1:3900",
    domains: 2,
    state: "active",
    lastSync: "2026-03-30T12:55:00Z",
  },
];

export const domains: DomainRecord[] = [
  {
    id: "domain_1",
    hostname: "flowpanel.app",
    site: "marketing-site",
    tls: "managed",
    dns: "verified",
    lastEvent: "2026-03-30T14:14:00Z",
  },
  {
    id: "domain_2",
    hostname: "api.flowpanel.app",
    site: "billing-api",
    tls: "pending",
    dns: "waiting",
    lastEvent: "2026-03-30T13:52:00Z",
  },
  {
    id: "domain_3",
    hostname: "docs.flowpanel.app",
    site: "docs-panel",
    tls: "error",
    dns: "mismatch",
    lastEvent: "2026-03-29T20:10:00Z",
  },
  {
    id: "domain_4",
    hostname: "checkout.flowpanel.app",
    site: "checkout-service",
    tls: "managed",
    dns: "verified",
    lastEvent: "2026-03-30T12:59:00Z",
  },
];

export const jobs: JobRecord[] = [
  {
    id: "job_1",
    job: "config_reconcile",
    schedule: "*/5 * * * *",
    status: "idle",
    lastRun: "2026-03-30T14:10:00Z",
  },
  {
    id: "job_2",
    job: "domain_health_check",
    schedule: "*/15 * * * *",
    status: "running",
    lastRun: "2026-03-30T14:15:00Z",
  },
  {
    id: "job_3",
    job: "sync_retry",
    schedule: "*/10 * * * *",
    status: "failed",
    lastRun: "2026-03-30T13:50:00Z",
  },
];
