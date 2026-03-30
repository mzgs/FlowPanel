import { useQuery } from "@tanstack/react-query";
import { ArrowUpRight, Clock3, Server, Shield, TimerReset } from "lucide-react";
import { fetchBootstrap } from "@/api/bootstrap";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { formatDateTime } from "@/lib/format";
import { domains, jobs, runtimeCards, sites } from "@/data/mock";

export function DashboardPage() {
  const bootstrapQuery = useQuery({
    queryKey: ["bootstrap"],
    queryFn: fetchBootstrap,
  });

  const runtimeMeta = bootstrapQuery.data
    ? `${bootstrapQuery.data.environment} environment · admin ${bootstrapQuery.data.admin_listen_addr}`
    : "Loading runtime status";

  return (
    <>
      <PageHeader
        title="Overview"
        meta={runtimeMeta}
        actions={
          <>
            <Button variant="secondary">New site</Button>
            <Button>Sync now</Button>
          </>
        }
      />
      <div className="grid gap-5 px-5 py-5 md:px-8 xl:grid-cols-[minmax(0,1.5fr)_360px]">
        <div className="space-y-5">
          <section className="grid gap-4 lg:grid-cols-2">
            {runtimeCards.map((item) => (
              <article
                key={item.label}
                className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]"
              >
                <div className="mb-3 flex items-center justify-between gap-3">
                  <div className="text-[13px] font-medium text-[var(--app-text-muted)]">
                    {item.label}
                  </div>
                  <StatusBadge status={item.status} />
                </div>
                <div className="mb-2 text-[16px] font-semibold text-[var(--app-text)]">
                  {item.value}
                </div>
                <p className="text-[13px] leading-5 text-[var(--app-text-muted)]">
                  {item.detail}
                </p>
              </article>
            ))}
          </section>

          <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] shadow-[var(--app-shadow)]">
            <div className="flex items-center justify-between border-b border-[var(--app-border)] px-4 py-3">
              <div className="text-[14px] font-semibold text-[var(--app-text)]">
                Sites requiring attention
              </div>
              <Button variant="quiet" size="sm">View all</Button>
            </div>
            <div className="divide-y divide-[var(--app-border)]">
              {sites.map((site) => (
                <div
                  key={site.id}
                  className="grid gap-2 px-4 py-3 md:grid-cols-[minmax(0,1fr)_140px_120px] md:items-center"
                >
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <div className="text-[14px] font-medium">{site.name}</div>
                      <StatusBadge status={site.state} />
                    </div>
                    <div className="font-mono text-[12px] text-[var(--app-text-muted)]">
                      {site.upstream}
                    </div>
                  </div>
                  <div className="text-[13px] text-[var(--app-text-muted)]">
                    {site.domains} domains
                  </div>
                  <div className="text-[13px] text-[var(--app-text-muted)]">
                    {formatDateTime(site.lastSync)}
                  </div>
                </div>
              ))}
            </div>
          </section>
        </div>

        <aside className="space-y-5">
          <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
            <div className="mb-4 text-[14px] font-semibold text-[var(--app-text)]">
              Runtime
            </div>
            <div className="space-y-3 text-[13px]">
              <div className="flex items-start gap-3">
                <Server className="mt-0.5 h-4 w-4 text-[var(--app-text-muted)]" />
                <div className="space-y-1">
                  <div className="text-[var(--app-text)]">Admin API</div>
                  <div className="text-[var(--app-text-muted)]">
                    {bootstrapQuery.data?.status === "ok" ? "Reachable" : "Pending"}
                  </div>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <Shield className="mt-0.5 h-4 w-4 text-[var(--app-text-muted)]" />
                <div className="space-y-1">
                  <div className="text-[var(--app-text)]">TLS</div>
                  <div className="text-[var(--app-text-muted)]">
                    Certificate automation arrives with embedded proxy sync
                  </div>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <TimerReset className="mt-0.5 h-4 w-4 text-[var(--app-text-muted)]" />
                <div className="space-y-1">
                  <div className="text-[var(--app-text)]">Scheduler</div>
                  <div className="text-[var(--app-text-muted)]">
                    {bootstrapQuery.data?.cron_enabled ? "Enabled" : "Disabled"}
                  </div>
                </div>
              </div>
            </div>
          </section>

          <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
            <div className="mb-4 text-[14px] font-semibold text-[var(--app-text)]">
              Domain queue
            </div>
            <div className="space-y-3">
              {domains.map((domain) => (
                <div key={domain.id} className="space-y-1 border-b border-[var(--app-border)] pb-3 last:border-b-0 last:pb-0">
                  <div className="flex items-center justify-between gap-3">
                    <div className="text-[13px] font-medium text-[var(--app-text)]">
                      {domain.hostname}
                    </div>
                    <StatusBadge status={domain.tls} />
                  </div>
                  <div className="text-[12px] text-[var(--app-text-muted)]">
                    {domain.site}
                  </div>
                </div>
              ))}
            </div>
          </section>

          <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
            <div className="mb-4 text-[14px] font-semibold text-[var(--app-text)]">
              Recent jobs
            </div>
            <div className="space-y-3">
              {jobs.map((job) => (
                <div key={job.id} className="flex items-start justify-between gap-3 border-b border-[var(--app-border)] pb-3 last:border-b-0 last:pb-0">
                  <div className="space-y-1">
                    <div className="text-[13px] font-medium text-[var(--app-text)]">
                      {job.job}
                    </div>
                    <div className="font-mono text-[12px] text-[var(--app-text-muted)]">
                      {job.schedule}
                    </div>
                  </div>
                  <div className="space-y-1 text-right">
                    <StatusBadge status={job.status} />
                    <div className="flex items-center justify-end gap-1 text-[12px] text-[var(--app-text-muted)]">
                      <Clock3 className="h-3.5 w-3.5" />
                      {formatDateTime(job.lastRun)}
                    </div>
                  </div>
                </div>
              ))}
            </div>
            <div className="mt-4">
              <Button variant="secondary" className="w-full justify-between">
                Open job history
                <ArrowUpRight className="h-4 w-4" />
              </Button>
            </div>
          </section>
        </aside>
      </div>
    </>
  );
}
