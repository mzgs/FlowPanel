import { useParams } from "@tanstack/react-router";
import { useEffect, useState, type FormEvent } from "react";
import {
  fetchDomainLogs,
  type DomainLogRecord,
  type DomainLogType,
  type FetchDomainLogsInput,
} from "@/api/domain-logs";
import {
  LoaderCircle,
  PlayerPlay,
  RefreshCw,
  Search,
  TimerReset,
} from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatBytes } from "@/lib/format";
import { cn, getErrorMessage } from "@/lib/utils";

type FilterState = {
  type: DomainLogType;
  search: string;
  limit: string;
};

type ClientFilterState = {
  ip: string;
  status: string;
};

type ParsedLogRow = {
  id: string;
  hostname: string;
  type: Exclude<DomainLogType, "all">;
  timestamp: string | null;
  timestampLabel: string;
  ip: string;
  statusCode: string;
  message: string;
  agent: string;
  sizeLabel: string;
  raw: string;
  parseMode: "json" | "common" | "raw";
};

const initialFilters: FilterState = {
  type: "all",
  search: "",
  limit: "200",
};

const initialClientFilters: ClientFilterState = {
  ip: "",
  status: "",
};

const apacheMonths: Record<string, string> = {
  Jan: "01",
  Feb: "02",
  Mar: "03",
  Apr: "04",
  May: "05",
  Jun: "06",
  Jul: "07",
  Aug: "08",
  Sep: "09",
  Oct: "10",
  Nov: "11",
  Dec: "12",
};

const apacheCombinedLogPattern =
  /^(?<ip>\S+) \S+ \S+ \[(?<date>[^\]]+)] "(?<request>[^"]*)" (?<status>\d{3}) (?<size>\S+)(?: "(?<referer>[^"]*)" "(?<agent>[^"]*)")?/;

function buildRequestFilters(filters: FilterState, hostname: string): FetchDomainLogsInput {
  const limit = Number.parseInt(filters.limit, 10);
  return {
    hostname,
    type: filters.type,
    search: filters.search.trim() || undefined,
    limit: Number.isFinite(limit) ? limit : 200,
  };
}

function logTypeLabel(type: DomainLogRecord["type"]) {
  return type === "access" ? "Access" : "Error";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function readString(value: unknown): string | null {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim();
  return normalized === "" ? null : normalized;
}

function readNumber(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }

  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }

  return null;
}

function normalizeTimestamp(value: unknown): string | null {
  const numeric = readNumber(value);
  if (numeric !== null) {
    const milliseconds = numeric > 1_000_000_000_000 ? numeric : numeric * 1000;
    const timestamp = new Date(milliseconds);
    return Number.isNaN(timestamp.getTime()) ? null : timestamp.toISOString();
  }

  const stringValue = readString(value);
  if (!stringValue) {
    return null;
  }

  const timestamp = new Date(stringValue);
  return Number.isNaN(timestamp.getTime()) ? null : timestamp.toISOString();
}

function formatPreciseDateTime(value: string | null) {
  if (!value) {
    return "Unknown";
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Unknown";
  }

  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  const hours = String(date.getHours()).padStart(2, "0");
  const minutes = String(date.getMinutes()).padStart(2, "0");
  const seconds = String(date.getSeconds()).padStart(2, "0");

  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
}

function formatStatusCode(value: number | null) {
  return value === null ? "—" : String(value);
}

function formatSizeLabel(value: number | null) {
  return value === null ? "—" : formatBytes(value);
}

function parseApacheTimestamp(value: string) {
  const match = value.match(/^(\d{2})\/([A-Za-z]{3})\/(\d{4}):(\d{2}:\d{2}:\d{2}) ([+-]\d{2})(\d{2})$/);
  if (!match) {
    return null;
  }

  const [, day, monthName, year, time, tzHours, tzMinutes] = match;
  const month = apacheMonths[monthName];
  if (!month) {
    return null;
  }

  return normalizeTimestamp(`${year}-${month}-${day}T${time}${tzHours}:${tzMinutes}`);
}

function headerValue(headers: Record<string, unknown>, key: string) {
  const value = headers[key] ?? headers[key.toLowerCase()];
  if (Array.isArray(value)) {
    return readString(value[0]);
  }
  return readString(value);
}

function buildFallbackRow(log: DomainLogRecord, line: string, index: number): ParsedLogRow {
  return {
    id: `${log.hostname}-${log.type}-${index}`,
    hostname: log.hostname,
    type: log.type,
    timestamp: null,
    timestampLabel: "Unknown",
    ip: "—",
    statusCode: "—",
    message: line,
    agent: log.type === "access" ? "Unknown client" : "Server event",
    sizeLabel: "—",
    raw: line,
    parseMode: "raw",
  };
}

function parseJsonLine(log: DomainLogRecord, line: string, index: number): ParsedLogRow | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(line);
  } catch {
    return null;
  }

  if (!isRecord(parsed)) {
    return null;
  }

  const request = isRecord(parsed.request) ? parsed.request : {};
  const headers = isRecord(request.headers) ? request.headers : {};
  const remoteIp =
    readString(request.remote_ip) ??
    readString(parsed.remote_ip) ??
    readString(parsed.client_ip) ??
    "—";
  const statusCode =
    readNumber(parsed.status) ??
    readNumber(parsed.status_code) ??
    readNumber(parsed.code);
  const size =
    readNumber(parsed.size) ??
    readNumber(parsed.bytes_written) ??
    readNumber(parsed.response_size);
  const method = readString(request.method);
  const uri = readString(request.uri) ?? readString(request.path);
  const requestLine = [method, uri].filter(Boolean).join(" ");
  const messageBase = readString(parsed.msg) ?? readString(parsed.message) ?? "Log entry";
  const errorText = readString(parsed.error);
  const message =
    log.type === "access"
      ? requestLine || messageBase
      : [messageBase, requestLine || errorText].filter(Boolean).join(" • ");

  return {
    id: `${log.hostname}-${log.type}-${index}`,
    hostname: log.hostname,
    type: log.type,
    timestamp: normalizeTimestamp(parsed.ts ?? parsed.time ?? parsed.timestamp),
    timestampLabel: formatPreciseDateTime(normalizeTimestamp(parsed.ts ?? parsed.time ?? parsed.timestamp)),
    ip: remoteIp,
    statusCode: formatStatusCode(statusCode),
    message: message || line,
    agent:
      headerValue(headers, "User-Agent") ??
      readString(parsed.user_agent) ??
      (log.type === "access" ? "Unknown client" : readString(parsed.level) ?? "Server event"),
    sizeLabel: formatSizeLabel(size),
    raw: line,
    parseMode: "json",
  };
}

function parseCombinedLogLine(log: DomainLogRecord, line: string, index: number): ParsedLogRow | null {
  const match = line.match(apacheCombinedLogPattern);
  if (!match?.groups) {
    return null;
  }

  const sizeValue = match.groups.size === "-" ? null : readNumber(match.groups.size);

  return {
    id: `${log.hostname}-${log.type}-${index}`,
    hostname: log.hostname,
    type: log.type,
    timestamp: parseApacheTimestamp(match.groups.date),
    timestampLabel: formatPreciseDateTime(parseApacheTimestamp(match.groups.date)),
    ip: match.groups.ip || "—",
    statusCode: match.groups.status || "—",
    message: match.groups.request || line,
    agent: match.groups.agent || "Unknown client",
    sizeLabel: formatSizeLabel(sizeValue),
    raw: line,
    parseMode: "common",
  };
}

function parseLogLine(log: DomainLogRecord, line: string, index: number) {
  return parseJsonLine(log, line, index) ?? parseCombinedLogLine(log, line, index) ?? buildFallbackRow(log, line, index);
}

function flattenLogRows(logs: DomainLogRecord[]) {
  const rows: ParsedLogRow[] = [];

  logs.forEach((log) => {
    log.lines.forEach((line, index) => {
      rows.push(parseLogLine(log, line, index));
    });
  });

  return rows;
}

function statusCodeClassName(value: string) {
  const numeric = Number.parseInt(value, 10);
  if (!Number.isFinite(numeric)) {
    return "text-muted-foreground";
  }
  if (numeric >= 500) {
    return "text-red-400";
  }
  if (numeric >= 400) {
    return "text-amber-400";
  }
  if (numeric >= 300) {
    return "text-sky-400";
  }
  if (numeric >= 200) {
    return "text-emerald-400";
  }
  return "text-muted-foreground";
}

function applyClientFilters(rows: ParsedLogRow[], filters: ClientFilterState) {
  const ipNeedle = filters.ip.trim().toLowerCase();
  const statusNeedle = filters.status.trim();

  return rows.filter((row) => {
    if (ipNeedle && !row.ip.toLowerCase().includes(ipNeedle)) {
      return false;
    }

    if (statusNeedle && !row.statusCode.startsWith(statusNeedle)) {
      return false;
    }

    return true;
  });
}

export function LogsPage() {
  const { hostname } = useParams({ from: "/domains/$hostname/logs" });
  const [filters, setFilters] = useState<FilterState>(initialFilters);
  const [clientFilters, setClientFilters] = useState<ClientFilterState>(initialClientFilters);
  const [activeFilters, setActiveFilters] = useState<FetchDomainLogsInput>(() =>
    buildRequestFilters(initialFilters, hostname),
  );
  const [logs, setLogs] = useState<DomainLogRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [liveUpdates, setLiveUpdates] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  async function loadLogs(nextFilters: FetchDomainLogsInput, showSpinner: boolean) {
    if (showSpinner) {
      setRefreshing(true);
    }

    try {
      const payload = await fetchDomainLogs(nextFilters);
      setLogs(payload.logs);
      setLoadError(null);
      setActiveFilters({
        hostname,
        type: payload.filters.type,
        search: payload.filters.search || undefined,
        limit: payload.filters.limit,
      });
    } catch (error) {
      setLoadError(getErrorMessage(error, "Failed to load domain logs."));
    } finally {
      setLoading(false);
      if (showSpinner) {
        setRefreshing(false);
      }
    }
  }

  useEffect(() => {
    const nextFilters = buildRequestFilters(initialFilters, hostname);
    setFilters(initialFilters);
    setClientFilters(initialClientFilters);
    setLiveUpdates(false);
    setLoading(true);
    setActiveFilters(nextFilters);
    void loadLogs(nextFilters, false);
  }, [hostname]);

  useEffect(() => {
    if (!liveUpdates) {
      return undefined;
    }

    const intervalId = window.setInterval(() => {
      void loadLogs(activeFilters, false);
    }, 5000);

    return () => window.clearInterval(intervalId);
  }, [activeFilters, liveUpdates]);

  function handleApplyFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void loadLogs(buildRequestFilters(filters, hostname), true);
  }

  function handleResetFilters() {
    setFilters(initialFilters);
    setClientFilters(initialClientFilters);
    setLiveUpdates(false);
    void loadLogs(buildRequestFilters(initialFilters, hostname), true);
  }

  const allRows = flattenLogRows(logs);
  const visibleRows = applyClientFilters(allRows, clientFilters);
  const rawRows = allRows.filter((row) => row.parseMode === "raw").length;
  const unavailableLogs = logs.filter((log) => !log.available || Boolean(log.read_error));

  return (
    <>
      <PageHeader
        title={(
          <span className="flex flex-wrap items-baseline gap-2">
            <span>Logs of</span>
            <span className="text-primary">{hostname}</span>
          </span>
        )}
        actions={(
          <>
            <Button
              type="button"
              variant={liveUpdates ? "default" : "outline"}
              onClick={() => setLiveUpdates((current) => !current)}
            >
              <PlayerPlay className="h-4 w-4" />
              {liveUpdates ? "Stop live updates" : "Start live updates"}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => void loadLogs(activeFilters, true)}
              disabled={refreshing}
            >
              {refreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              Refresh
            </Button>
            <Button type="button" variant="outline" onClick={handleResetFilters} disabled={refreshing}>
              <TimerReset className="h-4 w-4" />
              Reset
            </Button>
            <Select
              value={filters.type}
              onValueChange={(value) => setFilters((current) => ({ ...current, type: value as DomainLogType }))}
            >
              <SelectTrigger className="w-[150px] bg-card">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All logs</SelectItem>
                <SelectItem value="access">Access only</SelectItem>
                <SelectItem value="error">Error only</SelectItem>
              </SelectContent>
            </Select>
            <Select
              value={filters.limit}
              onValueChange={(value) => setFilters((current) => ({ ...current, limit: value }))}
            >
              <SelectTrigger className="w-[126px] bg-card">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="50">50 rows</SelectItem>
                <SelectItem value="100">100 rows</SelectItem>
                <SelectItem value="200">200 rows</SelectItem>
                <SelectItem value="500">500 rows</SelectItem>
              </SelectContent>
            </Select>
          </>
        )}
      />

      <div className="px-4 pb-6 sm:px-6 lg:px-8">
        <section className="overflow-hidden rounded-2xl border border-border bg-card/80 shadow-sm">
          <form
            className="border-b border-border bg-background/40 px-3 py-3 sm:px-4"
            onSubmit={handleApplyFilters}
          >
            <div className="grid gap-3 lg:grid-cols-[180px_140px_minmax(0,1fr)_auto]">
              <Input
                value={clientFilters.ip}
                onChange={(event) => setClientFilters((current) => ({ ...current, ip: event.target.value }))}
                placeholder="IP"
                aria-label="Filter visible rows by IP"
                className="bg-card"
              />

              <Input
                value={clientFilters.status}
                onChange={(event) => setClientFilters((current) => ({ ...current, status: event.target.value }))}
                placeholder="Code"
                aria-label="Filter visible rows by status code"
                className="bg-card"
              />

              <div className="relative">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={filters.search}
                  onChange={(event) => setFilters((current) => ({ ...current, search: event.target.value }))}
                  placeholder="Message, path, IP, or any raw log text"
                  aria-label="Search server log lines"
                  className="bg-card pl-9"
                />
              </div>

              <Button type="submit" disabled={refreshing}>
                Apply
              </Button>
            </div>
          </form>

          <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-3 text-sm text-muted-foreground sm:px-4">
            <span>{visibleRows.length} entries</span>
            <span className="text-border">•</span>
            <span>{logs.length} log streams</span>
            {rawRows > 0 ? (
              <>
                <span className="text-border">•</span>
                <span>{rawRows} raw lines</span>
              </>
            ) : null}
            {liveUpdates ? <Badge>Live</Badge> : null}
            {activeFilters.search ? <Badge variant="outline">search: {activeFilters.search}</Badge> : null}
            {clientFilters.ip ? <Badge variant="outline">ip: {clientFilters.ip}</Badge> : null}
            {clientFilters.status ? <Badge variant="outline">code: {clientFilters.status}</Badge> : null}
          </div>

          {loadError ? (
            <div className="border-b border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-[var(--app-danger)]">
              {loadError}
            </div>
          ) : null}

          {unavailableLogs.length > 0 ? (
            <div className="border-b border-border bg-background/30 px-4 py-3 text-sm text-muted-foreground">
              {unavailableLogs.length} log stream{unavailableLogs.length === 1 ? "" : "s"} unavailable or unreadable.
            </div>
          ) : null}

          <div className="max-h-[calc(100vh-22rem)] min-h-[26rem] overflow-auto">
            <Table className="min-w-[1080px]">
              <TableHeader className="sticky top-0 z-10 bg-card/95 backdrop-blur supports-[backdrop-filter]:bg-card/85">
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-[170px] px-3">Date</TableHead>
                  <TableHead className="w-[96px] px-3">Type</TableHead>
                  <TableHead className="w-[158px] px-3">IP</TableHead>
                  <TableHead className="w-[82px] px-3">Code</TableHead>
                  <TableHead className="min-w-[420px] px-3">Message</TableHead>
                  <TableHead className="w-[220px] px-3">Agent</TableHead>
                  <TableHead className="w-[96px] px-3">Size</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading ? (
                  <TableRow className="hover:bg-transparent">
                    <TableCell colSpan={7} className="h-64 text-center text-sm text-muted-foreground">
                      Loading logs...
                    </TableCell>
                  </TableRow>
                ) : visibleRows.length === 0 ? (
                  <TableRow className="hover:bg-transparent">
                    <TableCell colSpan={7} className="h-64 text-center text-sm text-muted-foreground">
                      No log entries matched the current filters for this domain.
                    </TableCell>
                  </TableRow>
                ) : (
                  visibleRows.map((row) => (
                    <TableRow
                      key={row.id}
                      className={cn(
                        "border-border/70 align-top",
                        row.parseMode === "raw" ? "bg-background/20" : "bg-transparent",
                      )}
                    >
                      <TableCell className="px-3 py-3 text-[13px] text-muted-foreground">
                        {row.timestampLabel}
                      </TableCell>
                      <TableCell className="px-3 py-3">
                        <Badge
                          variant={row.type === "access" ? "secondary" : "outline"}
                          className="font-medium"
                        >
                          {logTypeLabel(row.type)}
                        </Badge>
                      </TableCell>
                      <TableCell className="px-3 py-3 font-mono text-[13px] text-foreground">
                        {row.ip}
                      </TableCell>
                      <TableCell className={cn("px-3 py-3 font-mono text-[13px]", statusCodeClassName(row.statusCode))}>
                        {row.statusCode}
                      </TableCell>
                      <TableCell className="max-w-0 px-3 py-3">
                        <div className="space-y-1">
                          <div className="whitespace-normal break-words text-[13px] text-foreground">
                            {row.message}
                          </div>
                          {row.parseMode === "raw" ? (
                            <div className="line-clamp-2 break-all font-mono text-xs text-muted-foreground">
                              {row.raw}
                            </div>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell className="max-w-[220px] px-3 py-3">
                        <div className="line-clamp-2 whitespace-normal break-words text-[13px] text-muted-foreground">
                          {row.agent}
                        </div>
                      </TableCell>
                      <TableCell className="px-3 py-3 text-[13px] text-muted-foreground">
                        {row.sizeLabel}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </section>
      </div>
    </>
  );
}
