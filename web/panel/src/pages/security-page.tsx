import { useEffect, useState } from "react";
import {
  fetchFirewall,
  reconcileFirewall,
  setFirewallEnabled,
  type FirewallStatus,
} from "@/api/firewall";
import { LoaderCircle, RefreshCw, ShieldCheck } from "@/components/icons/lucide-icons";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

function formatPort(entry: { port: number; end_port?: number; protocol: string }) {
  return `${entry.port}${entry.end_port && entry.end_port !== entry.port ? `–${entry.end_port}` : ""}/${entry.protocol}`;
}

export function SecurityPage() {
  const [firewall, setFirewall] = useState<FirewallStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState<"toggle" | "reconcile" | null>(null);
  const [error, setError] = useState<string | null>(null);

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

  return (
    <>
      <PageHeader
        title="Security"
        meta="Default-deny inbound firewall rules managed by FlowPanel."
        actions={
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading || pending !== null}>
            <RefreshCw className={loading ? "animate-spin" : ""} />
            Refresh
          </Button>
        }
      />
      <div className="px-4 pb-6 sm:px-6 lg:px-8">
        <section className="rounded-xl border border-border bg-card shadow-sm">
          {loading ? (
            <div className="flex items-center gap-2 px-4 py-5 text-sm text-muted-foreground">
              <LoaderCircle className="animate-spin" />
              Loading firewall status...
            </div>
          ) : error || !firewall ? (
            <div className="px-4 py-5 text-sm text-destructive">{error || "Firewall status is unavailable."}</div>
          ) : (
            <>
              <div className="flex flex-col gap-3 border-b border-border px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
                <div className="flex min-w-0 items-start gap-3">
                  <div className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                    <ShieldCheck />
                  </div>
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="text-base font-semibold">Managed firewall</h2>
                      <Badge variant={firewall.active ? "default" : firewall.enabled ? "destructive" : "outline"}>
                        {firewall.active ? "Active" : firewall.enabled ? "Needs attention" : "Disabled"}
                      </Badge>
                      {firewall.backend ? <Badge variant="secondary">{firewall.backend}</Badge> : null}
                    </div>
                    <p className="mt-1 text-sm text-muted-foreground">
                      Keeps SSH, HTTP, HTTPS, the panel, enabled FTP ports, and public Docker mappings reachable.
                    </p>
                  </div>
                </div>
                <div className="flex shrink-0 gap-2">
                  {firewall.enabled ? (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => void reconcile()}
                      disabled={!firewall.supported || pending !== null}
                    >
                      {pending === "reconcile" ? <LoaderCircle className="animate-spin" /> : <RefreshCw />}
                      Reconcile
                    </Button>
                  ) : null}
                  <Button
                    variant={firewall.enabled ? "destructive" : "default"}
                    size="sm"
                    onClick={() => void updateEnabled()}
                    disabled={!firewall.supported || pending !== null}
                  >
                    {pending === "toggle" ? <LoaderCircle className="animate-spin" /> : null}
                    {firewall.enabled ? "Disable" : "Enable"}
                  </Button>
                </div>
              </div>

              {firewall.notice ? (
                <div className="border-b border-border bg-muted/40 px-4 py-2.5 text-sm text-muted-foreground">
                  {firewall.notice}
                </div>
              ) : null}

              <div className="grid gap-0 md:grid-cols-2">
                <div className="px-4 py-4 md:border-r md:border-border">
                  <h3 className="text-sm font-medium">Allowed inbound ports</h3>
                  <div className="mt-3 flex flex-wrap gap-2">
                    {firewall.allowed.length ? firewall.allowed.map((entry) => (
                      <Badge key={`${entry.protocol}-${entry.port}-${entry.end_port || ""}`} variant="outline">
                        {formatPort(entry)} · {entry.source}
                      </Badge>
                    )) : <span className="text-sm text-muted-foreground">No managed ports.</span>}
                  </div>
                </div>
                <div className="border-t border-border px-4 py-4 md:border-t-0">
                  <h3 className="text-sm font-medium">Public Docker mappings</h3>
                  <div className="mt-3 flex flex-wrap gap-2">
                    {firewall.docker_ports.length ? firewall.docker_ports.map((entry) => (
                      <Badge key={`${entry.protocol}-${entry.port}`} variant="secondary">
                        {formatPort(entry)}
                      </Badge>
                    )) : <span className="text-sm text-muted-foreground">No running public mappings.</span>}
                  </div>
                  <p className="mt-3 text-xs leading-5 text-muted-foreground">
                    Loopback Docker bindings stay private. Reverse proxies use the existing HTTP and HTTPS ports.
                  </p>
                </div>
              </div>
            </>
          )}
        </section>
      </div>
    </>
  );
}
