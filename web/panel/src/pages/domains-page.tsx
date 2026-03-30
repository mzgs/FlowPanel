import { useEffect, useRef, useState } from "react";
import { Plus } from "lucide-react";
import {
  createDomain,
  fetchDomains,
  type DomainApiError,
  type DomainKind,
  type DomainRecord,
} from "@/api/domains";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatDateTime } from "@/lib/format";

type FormState = {
  hostname: string;
  kind: DomainKind;
  target: string;
};

type FormErrors = {
  hostname?: string;
  target?: string;
};

const domainKinds: DomainKind[] = [
  "Static site",
  "Php site",
  "App",
  "Reverse proxy",
];

const initialFormState: FormState = {
  hostname: "",
  kind: "Static site",
  target: "",
};

const kindConfig: Record<
  DomainKind,
  {
    targetLabel?: string;
    targetPlaceholder?: string;
    helpText: string;
  }
> = {
  "Static site": {
    helpText: "FlowPanel uses the default site directory automatically.",
  },
  "Php site": {
    helpText: "FlowPanel uses the default PHP public directory automatically and requires PHP-FPM to be ready in Overview.",
  },
  App: {
    targetLabel: "Internal port",
    targetPlaceholder: "3000",
    helpText: "Traffic will be forwarded to the selected local application port.",
  },
  "Reverse proxy": {
    targetLabel: "Upstream URL",
    targetPlaceholder: "http://127.0.0.1:8080",
    helpText: "Requests will be proxied to this upstream service.",
  },
};

function normalizeHostname(value: string) {
  return value.trim().toLowerCase().replace(/\.$/, "");
}

function validateHostname(value: string) {
  if (!value) {
    return "Hostname is required.";
  }

  if (value.includes("://")) {
    return "Enter a hostname, not a full URL.";
  }

  if (/[\/\s]/.test(value)) {
    return "Hostname must not contain spaces or paths.";
  }

  if (!/^[a-z0-9.-]+$/i.test(value)) {
    return "Hostname can contain only letters, numbers, dots, and hyphens.";
  }

  return undefined;
}

function validateTarget(kind: DomainKind, value: string) {
  const trimmed = value.trim();

  if (kind === "App") {
    if (!trimmed) {
      return "Internal port is required.";
    }

    const port = Number(trimmed);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      return "Enter a valid port between 1 and 65535.";
    }
  }

  if (kind === "Reverse proxy") {
    if (!trimmed) {
      return "Upstream URL is required.";
    }

    if (!/^https?:\/\//i.test(trimmed)) {
      return "Enter a full upstream URL starting with http:// or https://.";
    }

    try {
      const parsed = new URL(trimmed);
      if (
        parsed.username ||
        parsed.password ||
        (parsed.pathname && parsed.pathname !== "/") ||
        parsed.search ||
        parsed.hash
      ) {
        return "Enter an upstream origin without credentials, paths, queries, or fragments.";
      }
    } catch {
      return "Enter a full upstream URL starting with http:// or https://.";
    }
  }

  return undefined;
}

function isSiteBackedKind(kind: DomainKind) {
  return kind === "Static site" || kind === "Php site";
}

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

export function DomainsPage() {
  const [domains, setDomains] = useState<DomainRecord[]>([]);
  const [form, setForm] = useState<FormState>(initialFormState);
  const [errors, setErrors] = useState<FormErrors>({});
  const [formOpen, setFormOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const hostnameInputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    let active = true;

    async function loadDomains() {
      try {
        const payload = await fetchDomains();
        if (!active) {
          return;
        }

        setDomains(payload.domains);
        setLoadError(null);
      } catch (error) {
        if (!active) {
          return;
        }

        setLoadError(getErrorMessage(error, "Failed to load domains."));
      } finally {
        if (active) {
          setLoading(false);
        }
      }
    }

    loadDomains();

    return () => {
      active = false;
    };
  }, []);

  const config = kindConfig[form.kind];

  function openForm() {
    setFormError(null);
    setFormOpen(true);
  }

  function resetForm() {
    setForm(initialFormState);
    setErrors({});
    setFormError(null);
  }

  function closeForm() {
    if (submitting) {
      return;
    }

    resetForm();
    setFormOpen(false);
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) {
      closeForm();
      return;
    }

    openForm();
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const hostname = normalizeHostname(form.hostname);
    const target = form.target.trim();
    const nextErrors: FormErrors = {
      hostname: validateHostname(hostname),
      target: isSiteBackedKind(form.kind)
        ? undefined
        : validateTarget(form.kind, target),
    };

    if (
      !nextErrors.hostname &&
      domains.some((domain) => domain.hostname === hostname)
    ) {
      nextErrors.hostname = "This hostname already exists.";
    }

    setErrors(nextErrors);
    if (nextErrors.hostname || nextErrors.target) {
      return;
    }

    setSubmitting(true);
    setFormError(null);

    try {
      const createdDomain = await createDomain({
        hostname,
        kind: form.kind,
        target: isSiteBackedKind(form.kind) ? "" : target,
      });

      setDomains((current) => [createdDomain, ...current]);
      resetForm();
      setFormOpen(false);
    } catch (error) {
      const domainError = error as DomainApiError;
      if (domainError.fieldErrors) {
        setErrors({
          hostname: domainError.fieldErrors.hostname,
          target: domainError.fieldErrors.target,
        });
      }

      setFormError(getErrorMessage(error, "Failed to create domain."));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={formOpen} onOpenChange={handleOpenChange}>
      <PageHeader
        title="Domains"
        meta={
          loading
            ? "Loading domains..."
            : domains.length
              ? `${domains.length} domain${domains.length === 1 ? "" : "s"} configured.`
              : "No domains have been added yet."
        }
        actions={
          <Button type="button" onClick={openForm}>
            <Plus className="h-4 w-4" />
            Add domain
          </Button>
        }
      />

      <div className="px-5 py-5 md:px-8">
        <div className="space-y-5">
          {loadError ? (
            <section className="rounded-[10px] border border-[var(--app-danger)]/40 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
              {loadError}
            </section>
          ) : null}

          <section className="overflow-hidden rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)]">
            <div className="border-b border-[var(--app-border)] px-5 py-4">
              <div className="text-[14px] font-medium text-[var(--app-text)]">
                Domain list
              </div>
            </div>

            {loading ? (
              <div className="px-5 py-10 text-[13px] text-[var(--app-text-muted)]">
                Loading domains...
              </div>
            ) : domains.length ? (
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead>Hostname</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Target</TableHead>
                    <TableHead>Created</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {domains.map((domain) => (
                    <TableRow key={domain.id}>
                      <TableCell className="font-medium text-[var(--app-text)]">
                        {domain.hostname}
                      </TableCell>
                      <TableCell>{domain.kind}</TableCell>
                      <TableCell className="font-mono text-[12px] text-[var(--app-text-muted)]">
                        {domain.target}
                      </TableCell>
                      <TableCell className="text-[12px] text-[var(--app-text-muted)]">
                        {formatDateTime(domain.created_at)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <div className="px-5 py-10">
                <div className="max-w-xl space-y-3">
                  <p className="text-[14px] text-[var(--app-text)]">
                    No domains configured.
                  </p>
                  <p className="text-[13px] leading-6 text-[var(--app-text-muted)]">
                    Click <span className="font-medium text-[var(--app-text)]">Add domain</span>{" "}
                    to create the first entry.
                  </p>
                </div>
              </div>
            )}
          </section>
        </div>
      </div>

      <DialogContent
        onOpenAutoFocus={(event) => {
          event.preventDefault();
          hostnameInputRef.current?.focus();
        }}
        onEscapeKeyDown={(event) => {
          if (submitting) {
            event.preventDefault();
          }
        }}
        onPointerDownOutside={(event) => {
          if (submitting) {
            event.preventDefault();
          }
        }}
      >
        <DialogHeader>
          <DialogTitle>New domain</DialogTitle>
          <DialogDescription>
            Define the hostname and route target. Static and PHP domains use the
            default directories automatically.
          </DialogDescription>
        </DialogHeader>

        {formError ? (
          <section className="rounded-[10px] border border-[var(--app-danger)]/40 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
            {formError}
          </section>
        ) : null}

        <form onSubmit={handleSubmit} className="space-y-5">
          <div className="grid gap-4 md:grid-cols-[200px_minmax(0,1fr)]">
            <div className="space-y-2">
              <label
                htmlFor="domain-kind"
                className="text-[13px] font-medium text-[var(--app-text)]"
              >
                Domain type
              </label>
              <select
                id="domain-kind"
                value={form.kind}
                onChange={(event) => {
                  const kind = event.target.value as DomainKind;
                  setForm((current) => ({
                    ...current,
                    kind,
                    target: "",
                  }));
                  setErrors((current) => ({
                    ...current,
                    target: undefined,
                  }));
                }}
                className="flex h-9 w-full rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] px-3 text-[14px] text-[var(--app-text)] outline-none transition-colors duration-150 focus:border-[var(--app-border-strong)] focus:ring-2 focus:ring-[var(--app-accent)]/20"
              >
                {domainKinds.map((kind) => (
                  <option key={kind} value={kind}>
                    {kind}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-2">
              <label
                htmlFor="domain-hostname"
                className="text-[13px] font-medium text-[var(--app-text)]"
              >
                Hostname
              </label>
              <Input
                id="domain-hostname"
                ref={hostnameInputRef}
                value={form.hostname}
                onChange={(event) => {
                  setForm((current) => ({
                    ...current,
                    hostname: event.target.value,
                  }));
                  if (errors.hostname) {
                    setErrors((current) => ({
                      ...current,
                      hostname: undefined,
                    }));
                  }
                }}
                placeholder="example.com"
                autoComplete="off"
                aria-invalid={errors.hostname ? "true" : "false"}
                className={errors.hostname ? "border-[var(--app-danger)]" : ""}
              />
              {errors.hostname ? (
                <p className="text-[12px] text-[var(--app-danger)]">{errors.hostname}</p>
              ) : null}
            </div>
          </div>

          {isSiteBackedKind(form.kind) ? (
            <p className="text-[12px] text-[var(--app-text-muted)]">{config.helpText}</p>
          ) : (
            <div className="space-y-2">
              <label
                htmlFor="domain-target"
                className="text-[13px] font-medium text-[var(--app-text)]"
              >
                {config.targetLabel}
              </label>
              <Input
                id="domain-target"
                value={form.target}
                onChange={(event) => {
                  setForm((current) => ({
                    ...current,
                    target: event.target.value,
                  }));
                  if (errors.target) {
                    setErrors((current) => ({
                      ...current,
                      target: undefined,
                    }));
                  }
                }}
                placeholder={config.targetPlaceholder}
                autoComplete="off"
                aria-invalid={errors.target ? "true" : "false"}
                className={errors.target ? "border-[var(--app-danger)]" : ""}
              />
              {errors.target ? (
                <p className="text-[12px] text-[var(--app-danger)]">{errors.target}</p>
              ) : (
                <p className="text-[12px] text-[var(--app-text-muted)]">
                  {config.helpText}
                </p>
              )}
            </div>
          )}

          <DialogFooter className="border-t border-[var(--app-border)] pt-4">
            <div className="text-[12px] text-[var(--app-text-muted)]">
              Static and PHP domains use the default directories automatically.
            </div>
            <div className="flex items-center justify-end gap-2">
              <Button
                type="button"
                variant="secondary"
                onClick={closeForm}
                disabled={submitting}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={submitting}>
                {submitting ? "Creating..." : "Create domain"}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
