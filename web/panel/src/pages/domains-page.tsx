import { useEffect, useRef, useState } from "react";
import { Pencil, Plus, Trash2 } from "@/components/icons/tabler-icons";
import {
  createDomain,
  deleteDomain,
  fetchDomains,
  updateDomain,
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
import { Switch } from "@/components/ui/switch";
import { formatDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";

type FormState = {
  hostname: string;
  kind: DomainKind;
  target: string;
  cacheEnabled: boolean;
};

type FormErrors = {
  hostname?: string;
  kind?: string;
  target?: string;
};

type FormMode = "create" | "edit";

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
  cacheEnabled: false,
};

const hostnamePattern =
  /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])$/i;

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
    helpText: "FlowPanel uses the default PHP site directory automatically and requires PHP-FPM to be ready in Overview.",
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
    return "Domain is required.";
  }

  if (value.includes("://")) {
    return "Enter a domain, not a full URL.";
  }

  if (/[\/\s]/.test(value)) {
    return "Domain must not contain spaces or paths.";
  }

  if (!/^[a-z0-9.-]+$/i.test(value)) {
    return "Domain can contain only letters, numbers, dots, and hyphens.";
  }

  if (!hostnamePattern.test(value)) {
    return "Enter a valid domain like example.com.";
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
  const [resetOnClose, setResetOnClose] = useState(false);
  const [formMode, setFormMode] = useState<FormMode>("create");
  const [editingDomainId, setEditingDomainId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [deletingDomainId, setDeletingDomainId] = useState<string | null>(null);
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

  const isEditing = formMode === "edit" && editingDomainId !== null;
  const config = kindConfig[form.kind];

  function resetForm() {
    setForm(initialFormState);
    setErrors({});
    setFormError(null);
    setFormMode("create");
    setEditingDomainId(null);
  }

  function openCreateForm() {
    setResetOnClose(false);
    resetForm();
    setFormOpen(true);
  }

  function openEditForm(domain: DomainRecord) {
    setResetOnClose(false);
    setForm({
      hostname: domain.hostname,
      kind: domain.kind,
      target: isSiteBackedKind(domain.kind) ? "" : domain.target,
      cacheEnabled: domain.cache_enabled,
    });
    setErrors({});
    setFormError(null);
    setFormMode("edit");
    setEditingDomainId(domain.id);
    setFormOpen(true);
  }

  function closeForm() {
    if (submitting) {
      return;
    }

    setResetOnClose(true);
    setFormOpen(false);
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) {
      closeForm();
      return;
    }

    setResetOnClose(false);
    setFormOpen(true);
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
      domains.some(
        (domain) =>
          domain.id !== editingDomainId && domain.hostname === hostname,
      )
    ) {
      nextErrors.hostname = "This domain already exists.";
    }

    setErrors(nextErrors);
    if (nextErrors.hostname || nextErrors.target) {
      return;
    }

    setSubmitting(true);
    setFormError(null);

    try {
      const input = {
        hostname,
        kind: form.kind,
        target: isSiteBackedKind(form.kind) ? "" : target,
        cache_enabled: form.cacheEnabled,
      };

      if (isEditing && editingDomainId) {
        const updatedDomain = await updateDomain(editingDomainId, input);
        setDomains((current) =>
          current.map((domain) =>
            domain.id === updatedDomain.id ? updatedDomain : domain,
          ),
        );
      } else {
        const createdDomain = await createDomain(input);
        setDomains((current) => [createdDomain, ...current]);
      }

      setLoadError(null);
      setResetOnClose(true);
      setFormOpen(false);
    } catch (error) {
      const domainError = error as DomainApiError;
      let hasFieldErrors = false;
      if (domainError.fieldErrors) {
        hasFieldErrors = Object.keys(domainError.fieldErrors).length > 0;
        setErrors({
          hostname: domainError.fieldErrors.hostname,
          kind: domainError.fieldErrors.kind,
          target: domainError.fieldErrors.target,
        });
      }

      setFormError(
        hasFieldErrors
          ? null
          : getErrorMessage(
              error,
              isEditing ? "Failed to update domain." : "Failed to create domain.",
            ),
      );
    } finally {
      setSubmitting(false);
    }
  }

  async function handleDelete(domain: DomainRecord) {
    if (submitting || deletingDomainId !== null) {
      return;
    }

    const confirmed = window.confirm(
      `Delete ${domain.hostname}? This removes it from FlowPanel and republishes the active routing.`,
    );
    if (!confirmed) {
      return;
    }

    setDeletingDomainId(domain.id);
    setLoadError(null);

    try {
      await deleteDomain(domain.id);
      setDomains((current) =>
        current.filter((currentDomain) => currentDomain.id !== domain.id),
      );
      if (editingDomainId === domain.id) {
        setResetOnClose(true);
        setFormOpen(false);
      }
    } catch (error) {
      setLoadError(
        getErrorMessage(error, `Failed to delete ${domain.hostname}.`),
      );
    } finally {
      setDeletingDomainId(null);
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
          <Button
            type="button"
            onClick={openCreateForm}
            disabled={deletingDomainId !== null}
          >
            <Plus className="h-4 w-4" />
            Add domain
          </Button>
        }
      />

      <div className="px-4 py-6 sm:px-6 lg:px-8">
        <div className="space-y-5">
          {loadError ? (
            <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
              {loadError}
            </section>
          ) : null}

          <section className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
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
                    <TableHead>Domain</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Cache</TableHead>
                    <TableHead>Target</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead className="w-[168px] text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {domains.map((domain) => (
                    <TableRow key={domain.id}>
                      <TableCell className="font-medium text-[var(--app-text)]">
                        {domain.hostname}
                      </TableCell>
                      <TableCell>{domain.kind}</TableCell>
                      <TableCell>
                        <span
                          className={cn(
                            "inline-flex rounded-full px-2.5 py-1 text-[11px] font-medium",
                            domain.cache_enabled
                              ? "bg-emerald-500/12 text-emerald-700"
                              : "bg-[var(--app-surface-muted)] text-[var(--app-text-muted)]",
                          )}
                        >
                          {domain.cache_enabled ? "Enabled" : "Off"}
                        </span>
                      </TableCell>
                      <TableCell className="font-mono text-[12px] text-[var(--app-text-muted)]">
                        {domain.target}
                      </TableCell>
                      <TableCell className="text-[12px] text-[var(--app-text-muted)]">
                        {formatDateTime(domain.created_at)}
                      </TableCell>
                      <TableCell className="w-[168px]">
                        <div className="flex items-center justify-end gap-2">
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => openEditForm(domain)}
                            disabled={deletingDomainId !== null}
                          >
                            <Pencil className="h-4 w-4" />
                            Edit
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              void handleDelete(domain);
                            }}
                            disabled={deletingDomainId !== null}
                            className="text-[var(--app-danger)] hover:bg-[var(--app-danger-soft)] hover:text-[var(--app-danger)]"
                          >
                            <Trash2 className="h-4 w-4" />
                            {deletingDomainId === domain.id ? "Deleting..." : "Delete"}
                          </Button>
                        </div>
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
        className="sm:max-w-xl"
        onAnimationEnd={(event) => {
          if (event.target !== event.currentTarget || formOpen || !resetOnClose) {
            return;
          }

          resetForm();
          setResetOnClose(false);
        }}
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
          <DialogTitle>{isEditing ? "Edit domain" : "New domain"}</DialogTitle>
          <DialogDescription>
            {isEditing
              ? "Update the route target and domain type. Domains stay fixed after creation."
              : "Define the domain and route target. Static and PHP domains use the default directories automatically."}
          </DialogDescription>
        </DialogHeader>

        {formError ? (
          <section className="rounded-[10px] border border-[var(--app-danger)]/40 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
            {formError}
          </section>
        ) : null}

        <form onSubmit={handleSubmit} className="space-y-5">
          <div className="space-y-2">
            <label
              htmlFor="domain-hostname"
              className="text-[13px] font-medium text-[var(--app-text)]"
            >
              Domain
            </label>
            <Input
              id="domain-hostname"
              ref={hostnameInputRef}
              value={form.hostname}
              readOnly={isEditing}
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
              className={
                errors.hostname
                  ? "border-[var(--app-danger)]"
                  : isEditing
                    ? "bg-[var(--app-surface-muted)]"
                    : ""
              }
            />
            {errors.hostname ? (
              <p className="text-[12px] text-[var(--app-danger)]">{errors.hostname}</p>
            ) : isEditing ? (
              <p className="text-[12px] text-[var(--app-text-muted)]">
                Domain cannot be changed after creation.
              </p>
            ) : null}
          </div>

          <div className="space-y-2">
            <label className="text-[13px] font-medium text-[var(--app-text)]">
              Domain type
            </label>
            <div
              role="group"
              aria-label="Domain type"
              className={cn(
                "flex flex-nowrap gap-2 overflow-x-auto rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-2",
                errors.kind ? "border-[var(--app-danger)]" : "",
              )}
            >
              {domainKinds.map((kind) => {
                const isActive = form.kind === kind;

                return (
                  <button
                    key={kind}
                    type="button"
                    onClick={() => {
                      setForm((current) => ({
                        ...current,
                        kind,
                        target: "",
                      }));
                      setErrors((current) => ({
                        ...current,
                        kind: undefined,
                        target: undefined,
                      }));
                    }}
                    aria-pressed={isActive}
                    className={cn(
                      "min-w-fit shrink-0 rounded-lg border px-3 py-2 text-[13px] font-medium whitespace-nowrap transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--app-text)]/20",
                      isActive
                        ? "border-[var(--app-text)]/10 bg-[var(--app-surface)] text-[var(--app-text)] shadow-sm"
                        : "border-transparent bg-transparent text-[var(--app-text-muted)] hover:border-[var(--app-border)] hover:bg-[var(--app-surface)] hover:text-[var(--app-text)]",
                    )}
                  >
                    {kind}
                  </button>
                );
              })}
            </div>
            {errors.kind ? (
              <p className="text-[12px] text-[var(--app-danger)]">{errors.kind}</p>
            ) : null}
          </div>

          {isSiteBackedKind(form.kind) ? null : (
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

          <div className="space-y-3 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3">
            <div className="flex items-center justify-between gap-4">
              <div className="space-y-1">
                <label
                  htmlFor="domain-cache-enabled"
                  className="text-[13px] font-medium text-[var(--app-text)]"
                >
                  Caddy cache
                </label>
                <p className="text-[12px] text-[var(--app-text-muted)]">
                  Cache eligible responses for this domain with Caddy&apos;s cache module.
                </p>
              </div>
              <Switch
                id="domain-cache-enabled"
                checked={form.cacheEnabled}
                disabled={submitting}
                onCheckedChange={(checked) => {
                  setForm((current) => ({
                    ...current,
                    cacheEnabled: checked,
                  }));
                }}
              />
            </div>
          </div>

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
                {submitting
                  ? isEditing
                    ? "Saving..."
                    : "Creating..."
                  : isEditing
                    ? "Save changes"
                    : "Create domain"}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
