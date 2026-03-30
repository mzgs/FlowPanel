import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { DataTable } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { formatDateTime } from "@/lib/format";
import { jobs, type JobRecord } from "@/data/mock";

const jobColumns: ColumnDef<JobRecord>[] = [
  {
    accessorKey: "job",
    header: "Job",
    cell: ({ row }) => (
      <div className="space-y-1">
        <div className="font-medium text-[var(--app-text)]">{row.original.job}</div>
        <div className="font-mono text-[12px] text-[var(--app-text-muted)]">
          {row.original.schedule}
        </div>
      </div>
    ),
  },
  {
    accessorKey: "status",
    header: "Status",
    cell: ({ row }) => <StatusBadge status={row.original.status} />,
  },
  {
    accessorKey: "lastRun",
    header: "Last run",
    cell: ({ row }) => (
      <div className="text-[var(--app-text-muted)]">
        {formatDateTime(row.original.lastRun)}
      </div>
    ),
  },
];

export function JobsPage() {
  return (
    <>
      <PageHeader
        title="Jobs"
        meta="Scheduled background work and retry behavior"
        actions={
          <>
            <Button variant="secondary">Edit schedules</Button>
            <Button>Run selected</Button>
          </>
        }
      />
      <div className="grid gap-5 px-5 py-5 md:px-8 xl:grid-cols-[minmax(0,1fr)_320px]">
        <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] shadow-[var(--app-shadow)]">
          <DataTable
            columns={jobColumns}
            data={jobs}
            emptyMessage="No jobs have been registered yet."
          />
        </section>

        <aside className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-4 shadow-[var(--app-shadow)]">
          <div className="mb-3 text-[14px] font-semibold">Execution notes</div>
          <ul className="space-y-2 text-[13px] leading-6 text-[var(--app-text-muted)]">
            <li>Jobs will be wrapped with structured logging and panic recovery.</li>
            <li>Run history will move from mock rows into SQLite once migrations land.</li>
            <li>Overlap protection will be added before reconciliation jobs become active.</li>
          </ul>
        </aside>
      </div>
    </>
  );
}
