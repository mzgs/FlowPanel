import { useEffect, useState } from "react";
import { fetchEvents, type ActivityEvent } from "@/api/events";
import { Copy, LoaderCircle, RefreshCw } from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatDateTime } from "@/lib/format";
import { copyTextToClipboard } from "@/lib/utils";
import { toast } from "sonner";

const messagePreviewLength = 200;

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

function formatAction(event: ActivityEvent) {
  return `${event.category} / ${event.action}`;
}

function getMessagePreview(message: string) {
  if (message.length <= messagePreviewLength) {
    return message;
  }

  return `${message.slice(0, messagePreviewLength).trimEnd()}...`;
}

function hasHiddenMessageContent(message: string) {
  return message.length > messagePreviewLength;
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
  const [selectedEvent, setSelectedEvent] = useState<ActivityEvent | null>(null);

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

  async function handleCopySelectedEventMessage() {
    if (!selectedEvent) {
      return;
    }

    try {
      await copyTextToClipboard(selectedEvent.message);
      toast.success("Full log copied.");
    } catch {
      toast.error("Failed to copy full log.");
    }
  }

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
                    <TableCell className="align-top">
                      <div className="max-w-3xl text-sm text-foreground whitespace-pre-wrap break-words">
                        {getMessagePreview(event.message)}
                      </div>
                      {hasHiddenMessageContent(event.message) ? (
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="mt-3 border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text)] hover:bg-[var(--app-bg-2)]"
                          onClick={() => setSelectedEvent(event)}
                        >
                          Show full log
                        </Button>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </section>
      </div>

      <Dialog open={selectedEvent !== null} onOpenChange={(open) => (!open ? setSelectedEvent(null) : null)}>
        <DialogContent className="h-[min(85vh,calc(100vh-2rem))] grid-rows-[auto_minmax(0,1fr)] overflow-hidden sm:max-w-4xl">
          <DialogHeader className="gap-3">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0 flex-1">
                <DialogTitle>{selectedEvent ? `${formatAction(selectedEvent)} log` : "Activity log"}</DialogTitle>
                <DialogDescription>
                  {selectedEvent
                    ? `${selectedEvent.resource_label || selectedEvent.resource_id} • ${formatDateTime(selectedEvent.created_at)}`
                    : "Full activity event log."}
                </DialogDescription>
              </div>

              {selectedEvent ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="shrink-0 self-start border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text)] hover:bg-[var(--app-bg-2)]"
                  onClick={() => void handleCopySelectedEventMessage()}
                >
                  <Copy className="h-4 w-4" />
                  Copy log
                </Button>
              ) : null}
            </div>
          </DialogHeader>

          {selectedEvent ? (
            <div className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
              <div className="border-b border-[var(--app-border)] bg-[var(--app-surface)] px-5 py-4 text-sm text-[var(--app-text-muted)]">
                <span className="font-medium text-[var(--app-text)]">{selectedEvent.status}</span>
                {" • "}
                {selectedEvent.actor}
                {" • "}
                {selectedEvent.resource_type}
              </div>

              <ScrollArea className="min-h-0 flex-1 bg-[var(--app-surface)]">
                <pre className="p-5 font-mono text-xs leading-5 whitespace-pre-wrap break-words text-[var(--app-text)] sm:p-6">
                  {selectedEvent.message}
                </pre>
              </ScrollArea>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  );
}
