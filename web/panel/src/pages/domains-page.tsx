import { useDeferredValue, useState } from "react";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { DataTable } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { formatDateTime } from "@/lib/format";
import { domains, type DomainRecord } from "@/data/mock";

const domainColumns: ColumnDef<DomainRecord>[] = [
  {
    accessorKey: "hostname",
    header: "Hostname",
    cell: ({ row }) => (
      <div className="space-y-1">
        <div className="font-medium text-[var(--app-text)]">{row.original.hostname}</div>
        <div className="text-[12px] text-[var(--app-text-muted)]">
          {row.original.site}
        </div>
      </div>
    ),
  },
  {
    accessorKey: "tls",
    header: "TLS",
    cell: ({ row }) => <StatusBadge status={row.original.tls} />,
  },
  {
    accessorKey: "dns",
    header: "DNS",
    cell: ({ row }) => <StatusBadge status={row.original.dns} />,
  },
  {
    accessorKey: "lastEvent",
    header: "Last event",
    cell: ({ row }) => (
      <div className="text-[var(--app-text-muted)]">
        {formatDateTime(row.original.lastEvent)}
      </div>
    ),
  },
];

export function DomainsPage() {
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);

  const filteredDomains = domains.filter((domain) => {
    const haystack = `${domain.hostname} ${domain.site}`.toLowerCase();
    return haystack.includes(deferredQuery.trim().toLowerCase());
  });

  return (
    <>
      <PageHeader
        title="Domains"
        meta="Hostname ownership, DNS state, and certificate progress"
        actions={
          <>
            <Button variant="secondary">Run diagnostics</Button>
            <Button>Add domain</Button>
          </>
        }
      />
      <div className="grid gap-5 px-5 py-5 md:px-8 xl:grid-cols-[minmax(0,1fr)_320px]">
        <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] shadow-[var(--app-shadow)]">
          <div className="flex flex-col gap-3 border-b border-[var(--app-border)] px-4 py-3 md:flex-row md:items-center md:justify-between">
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search by hostname or site"
              className="md:max-w-sm"
            />
            <div className="text-[13px] text-[var(--app-text-muted)]">
              {filteredDomains.length} domains
            </div>
          </div>
          <DataTable
            columns={domainColumns}
            data={filteredDomains}
            emptyMessage="No domains match the current filter."
          />
        </section>

        <aside className="space-y-5">
          <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
            <div className="mb-3 text-[14px] font-semibold">DNS checklist</div>
            <ul className="space-y-2 text-[13px] leading-6 text-[var(--app-text-muted)]">
              <li>Point A or CNAME records to the public listener.</li>
              <li>Wait for global propagation before forcing sync retries.</li>
              <li>Keep one primary hostname per site for operator clarity.</li>
            </ul>
          </section>

          <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
            <div className="mb-3 text-[14px] font-semibold">Next backend work</div>
            <ul className="space-y-2 text-[13px] leading-6 text-[var(--app-text-muted)]">
              <li>Persist domain ownership in SQLite.</li>
              <li>Validate duplicate hostnames and malformed inputs.</li>
              <li>Show live Caddy sync errors instead of placeholder states.</li>
            </ul>
          </section>
        </aside>
      </div>
    </>
  );
}
