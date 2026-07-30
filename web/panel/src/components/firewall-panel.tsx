import { useEffect, useState, type FormEvent } from "react";
import {
  fetchFirewall,
  reconcileFirewall,
  setFirewallEnabled,
  updateFirewallPort,
  type FirewallPort,
  type FirewallStatus,
} from "@/api/firewall";
import { LoaderCircle, Plus, RefreshCw, ShieldCheck, Trash2 } from "@/components/icons/lucide-icons";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

function formatPort(entry: Pick<FirewallPort, "port" | "end_port">) {
  return entry.end_port && entry.end_port !== entry.port ? `${entry.port}–${entry.end_port}` : String(entry.port);
}

function parsePort(value: string) {
  const [startText, endText, ...extra] = value.trim().split(/\s*-\s*/);
  const port = Number(startText);
  const endPort = endText ? Number(endText) : undefined;
  if (
    extra.length ||
    !Number.isInteger(port) ||
    port < 1 ||
    port > 65535 ||
    (endPort !== undefined && (!Number.isInteger(endPort) || endPort < port || endPort > 65535))
  ) {
    return null;
  }
  return { port, end_port: endPort };
}

export function FirewallPanel() {
  const [firewall, setFirewall] = useState<FirewallStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [port, setPort] = useState("");
  const [protocol, setProtocol] = useState<"tcp" | "udp">("tcp");

  async function load() {
    setLoading(true);
    setError(null);
    try {
      setFirewall(await fetchFirewall());
    } catch (loadError) {
      setError(getErrorMessage(loadError, "Firewall status could not be loaded."));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function updateEnabled() {
    if (!firewall) return;
    setPending("toggle");
    try {
      const next = await setFirewallEnabled(!firewall.enabled);
      setFirewall(next);
      toast.success(next.enabled ? "Managed firewall enabled." : "Managed firewall disabled.");
    } catch (updateError) {
      toast.error(getErrorMessage(updateError, "Firewall could not be updated."));
    } finally {
      setPending(null);
    }
  }

  async function reconcile() {
    setPending("reconcile");
    try {
      setFirewall(await reconcileFirewall());
      toast.success("Firewall rules reconciled.");
    } catch (reconcileError) {
      toast.error(getErrorMessage(reconcileError, "Firewall rules could not be reconciled."));
    } finally {
      setPending(null);
    }
  }

  async function openPort(event: FormEvent) {
    event.preventDefault();
    const parsed = parsePort(port);
    if (!parsed) {
      toast.error("Enter a port such as 8080 or a range such as 8000-8010.");
      return;
    }

    setPending("open");
    try {
      setFirewall(await updateFirewallPort({ ...parsed, protocol }, true));
      setPort("");
      toast.success(`${formatPort(parsed)}/${protocol} opened.`);
    } catch (openError) {
      toast.error(getErrorMessage(openError, "Port could not be opened."));
    } finally {
      setPending(null);
    }
  }

  async function closePort(entry: FirewallPort) {
    const key = `close:${entry.protocol}:${entry.port}:${entry.end_port || ""}`;
    setPending(key);
    try {
      setFirewall(await updateFirewallPort(entry, false));
      toast.success(`${formatPort(entry)}/${entry.protocol} closed.`);
    } catch (closeError) {
      toast.error(getErrorMessage(closeError, "Port could not be closed."));
    } finally {
      setPending(null);
    }
  }

  if (loading) {
    return (
      <div className="flex items-center gap-2 px-4 py-8 text-sm text-muted-foreground">
        <LoaderCircle className="h-4 w-4 animate-spin" />
        Loading firewall status...
      </div>
    );
  }

  if (error || !firewall) {
    return (
      <div className="flex flex-col items-start gap-3 px-4 py-6">
        <div className="text-sm text-destructive">{error || "Firewall status is unavailable."}</div>
        <Button variant="outline" size="sm" onClick={() => void load()}>Retry</Button>
      </div>
    );
  }

  return (
    <div>
      <div className="flex flex-col gap-3 border-b border-[var(--app-border)] px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          <div className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
            <ShieldCheck className="h-4 w-4" />
          </div>
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <div className="text-sm font-semibold">Managed firewall</div>
              <Badge variant={firewall.active ? "default" : firewall.enabled ? "destructive" : "outline"}>
                {firewall.active ? "Active" : firewall.enabled ? "Needs attention" : "Disabled"}
              </Badge>
              {firewall.backend ? <Badge variant="secondary">{firewall.backend}</Badge> : null}
            </div>
            <p className="mt-1 text-sm text-muted-foreground">
              Default-deny inbound rules with automatic protection for FlowPanel services.
            </p>
          </div>
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={pending !== null}>
            <RefreshCw className="h-4 w-4" />
            Refresh
          </Button>
          {firewall.enabled ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => void reconcile()}
              disabled={!firewall.supported || pending !== null}
            >
              {pending === "reconcile" ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              Reconcile
            </Button>
          ) : null}
          <Button
            variant={firewall.enabled ? "destructive" : "default"}
            size="sm"
            onClick={() => void updateEnabled()}
            disabled={!firewall.supported || pending !== null}
          >
            {pending === "toggle" ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
            {firewall.enabled ? "Disable" : "Enable"}
          </Button>
        </div>
      </div>

      {firewall.notice ? (
        <div className="border-b border-[var(--app-border)] bg-muted/40 px-4 py-2.5 text-sm text-muted-foreground">
          {firewall.notice}
        </div>
      ) : null}

      <form onSubmit={openPort} className="flex flex-col gap-3 border-b border-[var(--app-border)] px-4 py-4 sm:flex-row sm:items-end">
        <label className="block min-w-0 flex-1">
          <span className="mb-1.5 block text-xs font-medium text-muted-foreground">Port or range</span>
          <Input
            value={port}
            onChange={(event) => setPort(event.target.value)}
            placeholder="8080 or 8000-8010"
            inputMode="numeric"
            disabled={!firewall.supported || pending !== null}
          />
        </label>
        <label className="block sm:w-36">
          <span className="mb-1.5 block text-xs font-medium text-muted-foreground">Protocol</span>
          <Select value={protocol} onValueChange={(value) => setProtocol(value as "tcp" | "udp")} disabled={!firewall.supported || pending !== null}>
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="tcp">TCP</SelectItem>
              <SelectItem value="udp">UDP</SelectItem>
            </SelectContent>
          </Select>
        </label>
        <Button type="submit" size="sm" disabled={!firewall.supported || pending !== null || !port.trim()}>
          {pending === "open" ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
          Open port
        </Button>
      </form>

      <div className="px-4 py-4">
        <div className="mb-3">
          <div className="text-sm font-semibold">Open ports</div>
          <div className="text-xs text-muted-foreground">
            Service ports are maintained automatically. Custom ports can be closed here.
          </div>
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Port</TableHead>
              <TableHead>Protocol</TableHead>
              <TableHead>Source</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {firewall.allowed.map((entry) => {
              const key = `close:${entry.protocol}:${entry.port}:${entry.end_port || ""}`;
              const custom = entry.source === "Custom";
              return (
                <TableRow key={`${entry.protocol}:${entry.port}:${entry.end_port || ""}`}>
                  <TableCell className="font-mono text-xs">{formatPort(entry)}</TableCell>
                  <TableCell className="uppercase">{entry.protocol}</TableCell>
                  <TableCell>
                    <Badge variant={custom ? "secondary" : "outline"}>{entry.source}</Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    {custom ? (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        disabled={pending !== null}
                        onClick={() => void closePort(entry)}
                      >
                        {pending === key ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
                        Close
                      </Button>
                    ) : <span className="text-xs text-muted-foreground">Managed</span>}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
