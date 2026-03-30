import { useQuery } from "@tanstack/react-query";
import { fetchBootstrap } from "@/api/bootstrap";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";

export function SettingsPage() {
  const bootstrapQuery = useQuery({
    queryKey: ["bootstrap"],
    queryFn: fetchBootstrap,
  });

  return (
    <>
      <PageHeader
        title="Settings"
        meta="Runtime and deployment values exposed by the current backend skeleton"
      />
      <div className="grid gap-5 px-5 py-5 md:px-8 xl:grid-cols-2">
        <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
          <div className="mb-4 text-[14px] font-semibold">Runtime</div>
          <dl className="space-y-3 text-[13px]">
            <div className="flex items-center justify-between gap-4 border-b border-[var(--app-border)] pb-3">
              <dt className="text-[var(--app-text-muted)]">Environment</dt>
              <dd>{bootstrapQuery.data?.environment ?? "Loading"}</dd>
            </div>
            <div className="flex items-center justify-between gap-4 border-b border-[var(--app-border)] pb-3">
              <dt className="text-[var(--app-text-muted)]">Admin address</dt>
              <dd className="font-mono">{bootstrapQuery.data?.admin_listen_addr ?? "Loading"}</dd>
            </div>
            <div className="flex items-center justify-between gap-4">
              <dt className="text-[var(--app-text-muted)]">Cron</dt>
              <dd>
                <Badge variant={bootstrapQuery.data?.cron_enabled ? "success" : "warning"}>
                  {bootstrapQuery.data?.cron_enabled ? "Enabled" : "Disabled"}
                </Badge>
              </dd>
            </div>
          </dl>
        </section>

        <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
          <div className="mb-4 text-[14px] font-semibold">Next additions</div>
          <ul className="space-y-2 text-[13px] leading-6 text-[var(--app-text-muted)]">
            <li>Session management and admin user bootstrap.</li>
            <li>Proxy listener ownership and TLS automation settings.</li>
            <li>SQLite migration visibility and audit controls.</li>
          </ul>
        </section>
      </div>
    </>
  );
}
