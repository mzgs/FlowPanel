import { useEffect, useState } from "react";
import { fetchDiskSnapshot, type DiskSnapshot } from "@/api/system";
import { HardDrive, LoaderCircle, RefreshCw } from "@/components/icons/lucide-icons";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDateTime } from "@/lib/format";
import { getErrorMessage } from "@/lib/utils";

function usageTone(percent: number) {
  return percent >= 85 ? "bg-[var(--app-danger)]" : percent >= 70 ? "bg-[var(--app-warning)]" : "bg-[var(--app-ok)]";
}

export function DiskPanel() {
  const [snapshot, setSnapshot] = useState<DiskSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function load(signal?: AbortSignal) {
    setLoading(true);
    try {
      setSnapshot(await fetchDiskSnapshot(signal));
      setError(null);
    } catch (nextError) {
      if (!signal?.aborted) {
        setError(getErrorMessage(nextError, "Disk details could not be loaded."));
      }
    } finally {
      if (!signal?.aborted) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, []);

  if (loading && !snapshot) {
    return (
      <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-8 shadow-[var(--app-shadow)]">
        <div className="flex items-center gap-3 text-sm text-muted-foreground">
          <LoaderCircle className="h-4 w-4 animate-spin" />
          Scanning disk usage and largest files...
        </div>
      </section>
    );
  }

  if (!snapshot) {
    return (
      <section className="rounded-xl border border-destructive/40 bg-destructive/10 px-4 py-4 shadow-[var(--app-shadow)]">
        <div className="text-sm text-destructive">{error}</div>
        <Button className="mt-3" variant="outline" size="sm" onClick={() => void load()}>Retry</Button>
      </section>
    );
  }

  const primaryMount = snapshot.mounts.find((mount) => mount.mountpoint === snapshot.scanned_path) ?? snapshot.mounts[0];

  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
      <div className="flex flex-col gap-2 border-b border-[var(--app-border)] px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">Disk usage</div>
          <div className="text-sm text-[var(--app-text-muted)]">
            Scanned {snapshot.scanned_path} at {formatDateTime(snapshot.scanned_at)}
          </div>
        </div>
        <Button variant="outline" size="sm" disabled={loading} onClick={() => void load()}>
          {loading ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
          Rescan
        </Button>
      </div>

      {error ? <div className="border-b border-destructive/30 bg-destructive/10 px-4 py-2 text-sm text-destructive">{error}</div> : null}
      {!snapshot.scan_complete ? (
        <div className="border-b border-amber-500/30 bg-amber-500/10 px-4 py-2 text-sm text-amber-200">
          The scan reached its time limit. Results show the largest files found so far.
        </div>
      ) : null}

      <div className="grid border-b border-[var(--app-border)] sm:grid-cols-3">
        {[
          { label: "Used", value: primaryMount ? formatBytes(primaryMount.used_bytes) : "Unavailable" },
          { label: "Available", value: primaryMount ? formatBytes(primaryMount.free_bytes) : "Unavailable" },
          { label: "Total", value: primaryMount ? formatBytes(primaryMount.total_bytes) : "Unavailable" },
        ].map((item, index) => (
          <div key={item.label} className={`px-4 py-3 ${index ? "border-t border-[var(--app-border)] sm:border-l sm:border-t-0" : ""}`}>
            <div className="text-xs font-medium uppercase tracking-[0.12em] text-[var(--app-text-muted)]">{item.label}</div>
            <div className="mt-1 text-xl font-semibold tracking-tight text-[var(--app-text)]">{item.value}</div>
          </div>
        ))}
      </div>

      {primaryMount ? (
        <div className="border-b border-[var(--app-border)] px-4 py-3">
          <div className="mb-2 flex items-center justify-between gap-3 text-sm">
            <span className="flex min-w-0 items-center gap-2 font-medium"><HardDrive className="h-4 w-4" />{primaryMount.mountpoint}</span>
            <span className="text-[var(--app-text-muted)]">{primaryMount.used_percent.toFixed(1)}% used</span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-[var(--app-surface)]" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(primaryMount.used_percent)}>
            <div className={`h-full rounded-full ${usageTone(primaryMount.used_percent)}`} style={{ width: `${Math.min(100, primaryMount.used_percent)}%` }} />
          </div>
        </div>
      ) : null}

      <div className="border-b border-[var(--app-border)] px-4 py-3">
        <div className="mb-2 text-sm font-semibold text-[var(--app-text)]">Mounted volumes</div>
        <div className="overflow-x-auto">
          <Table>
            <TableHeader><TableRow><TableHead>Mount</TableHead><TableHead>Device</TableHead><TableHead>Filesystem</TableHead><TableHead>Used</TableHead><TableHead>Available</TableHead><TableHead className="text-right">Usage</TableHead></TableRow></TableHeader>
            <TableBody>
              {snapshot.mounts.map((mount) => (
                <TableRow key={`${mount.device}:${mount.mountpoint}`}>
                  <TableCell className="font-mono text-xs">{mount.mountpoint}</TableCell>
                  <TableCell className="max-w-[18rem] truncate text-xs text-muted-foreground">{mount.device}</TableCell>
                  <TableCell>{mount.filesystem || "—"}</TableCell>
                  <TableCell>{formatBytes(mount.used_bytes)}</TableCell>
                  <TableCell>{formatBytes(mount.free_bytes)}</TableCell>
                  <TableCell className="text-right font-medium">{mount.used_percent.toFixed(1)}%</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </div>

      <div className="px-4 py-3">
        <div className="mb-2 text-sm font-semibold text-[var(--app-text)]">Largest files</div>
        {snapshot.largest_files.length ? (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader><TableRow><TableHead>File</TableHead><TableHead>Modified</TableHead><TableHead className="text-right">Size</TableHead></TableRow></TableHeader>
              <TableBody>
                {snapshot.largest_files.map((file) => (
                  <TableRow key={file.path}>
                    <TableCell className="max-w-[48rem]"><div className="truncate font-mono text-xs" title={file.path}>{file.path}</div></TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">{formatDateTime(file.modified_at)}</TableCell>
                    <TableCell className="whitespace-nowrap text-right font-medium">{formatBytes(file.size_bytes)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        ) : <div className="py-5 text-sm text-muted-foreground">No readable files were found.</div>}
      </div>
    </section>
  );
}
