import { useDeferredValue, useState } from "react";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { DataTable } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { formatDateTime } from "@/lib/format";
import { sites, type SiteRecord } from "@/data/mock";

const siteColumns: ColumnDef<SiteRecord>[] = [
  {
    accessorKey: "name",
    header: "Site",
    cell: ({ row }) => (
      <div className="space-y-1">
        <div className="font-medium text-[var(--app-text)]">{row.original.name}</div>
        <div className="font-mono text-[12px] text-[var(--app-text-muted)]">
          {row.original.upstream}
        </div>
      </div>
    ),
  },
  {
    accessorKey: "domains",
    header: "Domains",
  },
  {
    accessorKey: "state",
    header: "State",
    cell: ({ row }) => <StatusBadge status={row.original.state} />,
  },
  {
    accessorKey: "lastSync",
    header: "Last sync",
    cell: ({ row }) => (
      <div className="text-[var(--app-text-muted)]">
        {formatDateTime(row.original.lastSync)}
      </div>
    ),
  },
];

export function SitesPage() {
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);

  const filteredSites = sites.filter((site) => {
    const haystack = `${site.name} ${site.upstream}`.toLowerCase();
    return haystack.includes(deferredQuery.trim().toLowerCase());
  });

  return (
    <>
      <PageHeader
        title="Sites"
        meta="Upstream applications and their current routing state"
        actions={
          <>
            <Button variant="secondary">Import</Button>
            <Button>Create site</Button>
          </>
        }
      />
      <div className="space-y-5 px-5 py-5 md:px-8">
        <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] shadow-[var(--app-shadow)]">
          <div className="flex flex-col gap-3 border-b border-[var(--app-border)] px-4 py-3 md:flex-row md:items-center md:justify-between">
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search by site name or upstream"
              className="md:max-w-sm"
            />
            <div className="text-[13px] text-[var(--app-text-muted)]">
              {filteredSites.length} of {sites.length} sites
            </div>
          </div>
          <DataTable
            columns={siteColumns}
            data={filteredSites}
            emptyMessage="No sites match the current filter."
          />
        </section>

        <section className="grid gap-5 xl:grid-cols-3">
          <article className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
            <div className="mb-2 text-[14px] font-semibold">Selection model</div>
            <p className="text-[13px] leading-6 text-[var(--app-text-muted)]">
              Row selection and edit drawers will land once the CRUD endpoints are implemented.
            </p>
          </article>
          <article className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
            <div className="mb-2 text-[14px] font-semibold">Validation</div>
            <p className="text-[13px] leading-6 text-[var(--app-text-muted)]">
              Upstream URL parsing, duplicate checks, and delete guards are scheduled after migrations and auth.
            </p>
          </article>
          <article className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
            <div className="mb-2 text-[14px] font-semibold">Operator flow</div>
            <p className="text-[13px] leading-6 text-[var(--app-text-muted)]">
              This screen is already structured around the final workflow: list, filter, inspect, then sync.
            </p>
          </article>
        </section>
      </div>
    </>
  );
}
