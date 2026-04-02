import { useEffect, useState } from "react";
import { fetchEvents, type ActivityEvent } from "@/api/events";
import { LoaderCircle, RefreshCw } from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatDateTime } from "@/lib/format";

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

function formatAction(event: ActivityEvent) {
  return `${event.category} / ${event.action}`;
}

function getStatusVariant(status: string) {
  if (status === "failed") {
    return "destructive" as const;
  }

  return "secondary" as const;
}

export function ActivityPage() {
  const [events, setEvents] = useState<ActivityEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  async function loadEvents(showSpinner: boolean) {
    if (showSpinner) {
      setRefreshing(true);
    }

    try {
      const payload = await fetchEvents();
      setEvents(payload.events);
      setLoadError(null);
    } catch (error) {
      setLoadError(getErrorMessage(error, "Failed to load activity."));
    } finally {
      setLoading(false);
      if (showSpinner) {
        setRefreshing(false);
      }
    }
  }

  useEffect(() => {
    void loadEvents(false);
  }, []);

  return (
    <>
      <PageHeader
        title="Activity"
        meta="Recent panel actions and runtime events recorded by the backend."
        actions={(
          <Button type="button" variant="outline" onClick={() => void loadEvents(true)} disabled={refreshing}>
            {refreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            Refresh
          </Button>
        )}
      />

      <div className="px-4 pb-6 sm:px-6 lg:px-8">
        <section className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
          {loadError ? (
            <div className="border-b border-[var(--app-border)] px-4 py-3 text-sm text-[var(--app-danger)]">
              {loadError}
            </div>
          ) : null}

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[160px]">Time</TableHead>
                <TableHead className="w-[180px]">Action</TableHead>
                <TableHead className="w-[220px]">Resource</TableHead>
                <TableHead className="w-[120px]">Status</TableHead>
                <TableHead>Message</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={5} className="h-40 text-center text-sm text-muted-foreground">
                    Loading activity...
                  </TableCell>
                </TableRow>
              ) : events.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="h-40 text-center text-sm text-muted-foreground">
                    No events recorded yet.
                  </TableCell>
                </TableRow>
              ) : (
                events.map((event) => (
                  <TableRow key={event.id}>
                    <TableCell className="align-top text-sm text-muted-foreground">
                      {formatDateTime(event.created_at)}
                    </TableCell>
                    <TableCell className="align-top">
                      <div className="font-medium text-foreground">{formatAction(event)}</div>
                      <div className="text-xs text-muted-foreground">{event.actor}</div>
                    </TableCell>
                    <TableCell className="align-top">
                      <div className="font-medium text-foreground">{event.resource_label || event.resource_id}</div>
                      <div className="break-all text-xs text-muted-foreground">{event.resource_type}</div>
                    </TableCell>
                    <TableCell className="align-top">
                      <Badge variant={getStatusVariant(event.status)}>{event.status}</Badge>
                    </TableCell>
                    <TableCell className="align-top text-sm text-foreground">{event.message}</TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </section>
      </div>
    </>
  );
}
