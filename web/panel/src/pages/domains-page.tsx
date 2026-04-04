import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import {
  Download,
  ExternalLink,
  FolderOpen,
  LoaderCircle,
  Pencil,
  Plus,
  Trash2,
} from "@/components/icons/tabler-icons";
import {
  createBackup,
  deleteBackup,
  fetchBackups,
  restoreBackup,
  type BackupRecord,
} from "@/api/backups";
import {
  createDomain,
  deleteDomain,
  fetchDomains,
  getDomainSiteUrl,
  updateDomain,
  type DomainApiError,
  type DomainKind,
  type DomainRecord,
} from "@/api/domains";
import { downloadEntry } from "@/api/files";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { BackupRecordsDialog } from "@/components/backup-records-dialog";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
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
import { getSiteHostnameFromBackupRecord } from "@/lib/backup-records";
import { getFilesPathFromDomainTarget } from "@/lib/domain-targets";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

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

const domainActionButtonClass =
  "inline-flex h-9 w-9 items-center justify-center rounded-lg text-[var(--app-text-muted)] transition hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)] disabled:cursor-not-allowed disabled:opacity-60";
const domainDangerActionButtonClass =
  "inline-flex h-9 w-9 items-center justify-center rounded-lg text-[var(--app-danger)] transition hover:bg-[var(--app-danger-soft)] hover:text-[var(--app-danger)] disabled:cursor-not-allowed disabled:opacity-60";
const domainActionIconStroke = 1.5;

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
    helpText:
      "FlowPanel uses the default PHP site directory automatically and requires PHP-FPM to be ready in Overview.",
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

function getDuplicateHostnameError(
  hostname: string,
  domains: DomainRecord[],
  editingDomainId: string | null,
) {
  return domains.some(
    (domain) => domain.id !== editingDomainId && domain.hostname === hostname,
  )
    ? "This domain already exists."
    : undefined;
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
  const [backups, setBackups] = useState<BackupRecord[]>([]);
  const [sitesBasePath, setSitesBasePath] = useState("");
  const [form, setForm] = useState<FormState>(initialFormState);
  const [errors, setErrors] = useState<FormErrors>({});
  const [formOpen, setFormOpen] = useState(false);
  const [resetOnClose, setResetOnClose] = useState(false);
  const [formMode, setFormMode] = useState<FormMode>("create");
  const [editingDomainId, setEditingDomainId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [backupsLoading, setBackupsLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [deletingDomainId, setDeletingDomainId] = useState<string | null>(null);
  const [deleteDomainCandidate, setDeleteDomainCandidate] = useState<DomainRecord | null>(null);
  const [creatingBackupDomainId, setCreatingBackupDomainId] = useState<string | null>(null);
  const [restoringBackupName, setRestoringBackupName] = useState<string | null>(null);
  const [restoredBackupName, setRestoredBackupName] = useState<string | null>(null);
  const [deletingBackupName, setDeletingBackupName] = useState<string | null>(null);
  const [createdBackupDomainId, setCreatedBackupDomainId] = useState<string | null>(null);
  const [downloadingDomainId, setDownloadingDomainId] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [backupsLoadError, setBackupsLoadError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [backupDialogDomain, setBackupDialogDomain] = useState<DomainRecord | null>(
    null,
  );
  const hostnameInputRef = useRef<HTMLInputElement | null>(null);
  const createdBackupTimeoutRef = useRef<number | null>(null);
  const restoredBackupTimeoutRef = useRef<number | null>(null);

  useEffect(() => {
    let active = true;

    async function loadData() {
      try {
        const [domainsResult, backupsResult] = await Promise.allSettled([
          fetchDomains(),
          fetchBackups(),
        ]);
        if (!active) {
          return;
        }

        if (domainsResult.status === "fulfilled") {
          setDomains(domainsResult.value.domains);
          setSitesBasePath(domainsResult.value.sites_base_path);
          setLoadError(null);
        } else {
          setLoadError(getErrorMessage(domainsResult.reason, "Failed to load domains."));
        }

        if (backupsResult.status === "fulfilled") {
          setBackups(backupsResult.value.backups);
          setBackupsLoadError(null);
        } else {
          setBackups([]);
          setBackupsLoadError(getErrorMessage(backupsResult.reason, "Failed to load backups."));
        }
      } finally {
        if (active) {
          setLoading(false);
          setBackupsLoading(false);
        }
      }
    }

    void loadData();

    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    return () => {
      if (createdBackupTimeoutRef.current !== null) {
        window.clearTimeout(createdBackupTimeoutRef.current);
      }
      if (restoredBackupTimeoutRef.current !== null) {
        window.clearTimeout(restoredBackupTimeoutRef.current);
      }
    };
  }, []);

  const isEditing = formMode === "edit" && editingDomainId !== null;
  const config = kindConfig[form.kind];
  const siteBackups = backups.reduce<Record<string, BackupRecord[]>>((groups, backup) => {
    const hostname = getSiteHostnameFromBackupRecord(backup);
    if (!hostname) {
      return groups;
    }

    if (!groups[hostname]) {
      groups[hostname] = [];
    }
    groups[hostname].push(backup);
    return groups;
  }, {});
  const selectedDomainBackups = backupDialogDomain
    ? siteBackups[backupDialogDomain.hostname] ?? []
    : [];
  const backupDialogCreating =
    backupDialogDomain !== null && creatingBackupDomainId === backupDialogDomain.id;
  const backupDialogCreated =
    backupDialogDomain !== null && createdBackupDomainId === backupDialogDomain.id;

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
      getDuplicateHostnameError(hostname, domains, editingDomainId)
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

  function handleDelete(domain: DomainRecord) {
    if (submitting || deletingDomainId !== null) {
      return;
    }

    setDeleteDomainCandidate(domain);
  }

  async function confirmDeleteDomain() {
    if (!deleteDomainCandidate) {
      return;
    }

    const domain = deleteDomainCandidate;
    setDeletingDomainId(domain.id);
    setLoadError(null);

    try {
      await deleteDomain(domain.id);
      setDomains((current) =>
        current.filter((currentDomain) => currentDomain.id !== domain.id),
      );
      setBackupDialogDomain((current) => (current?.id === domain.id ? null : current));
      if (editingDomainId === domain.id) {
        setResetOnClose(true);
        setFormOpen(false);
      }
    } catch (error) {
      setLoadError(getErrorMessage(error, `Failed to delete ${domain.hostname}.`));
    } finally {
      setDeletingDomainId(null);
      setDeleteDomainCandidate((current) => (current?.id === domain.id ? null : current));
    }
  }

  async function handleCreateBackup(domain: DomainRecord) {
    if (!isSiteBackedKind(domain.kind) || creatingBackupDomainId !== null) {
      return;
    }

    setCreatingBackupDomainId(domain.id);
    setCreatedBackupDomainId(null);

    try {
      const record = await createBackup({
        include_panel_data: false,
        include_sites: true,
        include_databases: false,
        site_hostnames: [domain.hostname],
      });
      setBackups((current) => [record, ...current.filter((item) => item.name !== record.name)]);
      setBackupsLoadError(null);
      if (createdBackupTimeoutRef.current !== null) {
        window.clearTimeout(createdBackupTimeoutRef.current);
      }
      setCreatedBackupDomainId(domain.id);
      createdBackupTimeoutRef.current = window.setTimeout(() => {
        setCreatedBackupDomainId((current) =>
          current === domain.id ? null : current,
        );
        createdBackupTimeoutRef.current = null;
      }, 1500);
      toast.success(`Created backup ${record.name}.`);
    } catch (error) {
      toast.error(
        getErrorMessage(error, `Failed to create backup for ${domain.hostname}.`),
      );
    } finally {
      setCreatingBackupDomainId(null);
    }
  }

  async function handleDownload(domain: DomainRecord, filesPath: string) {
    if (downloadingDomainId !== null) {
      return;
    }

    setDownloadingDomainId(domain.id);

    try {
      const fileName = await downloadEntry(filesPath);
      toast.success(`Downloaded ${fileName}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, `Failed to download ${domain.hostname}.`));
    } finally {
      setDownloadingDomainId(null);
    }
  }

  async function handleRestoreBackup(name: string) {
    if (restoringBackupName === name || deletingBackupName === name) {
      return;
    }

    setRestoringBackupName(name);
    setRestoredBackupName(null);

    try {
      await restoreBackup(name);
      if (restoredBackupTimeoutRef.current !== null) {
        window.clearTimeout(restoredBackupTimeoutRef.current);
      }
      setRestoredBackupName(name);
      restoredBackupTimeoutRef.current = window.setTimeout(() => {
        setRestoredBackupName((current) => (current === name ? null : current));
        restoredBackupTimeoutRef.current = null;
      }, 1500);
    } catch (error) {
      toast.error(getErrorMessage(error, `Failed to restore ${name}.`));
    } finally {
      setRestoringBackupName(null);
    }
  }

  async function handleDeleteBackup(name: string) {
    if (deletingBackupName === name || restoringBackupName === name) {
      return;
    }

    setDeletingBackupName(name);

    try {
      await deleteBackup(name);
      setBackups((current) => current.filter((item) => item.name !== name));
      toast.success(`Deleted backup ${name}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, `Failed to delete ${name}.`));
    } finally {
      setDeletingBackupName(null);
    }
  }

  return (
    <>
      <BackupRecordsDialog
        open={backupDialogDomain !== null}
        onOpenChange={(open) => {
          if (!open) {
            setBackupDialogDomain(null);
          }
        }}
        title={backupDialogDomain ? `${backupDialogDomain.hostname} backups` : "Domain backups"}
        backups={selectedDomainBackups}
        onCreateBackup={() => {
          if (backupDialogDomain) {
            void handleCreateBackup(backupDialogDomain);
          }
        }}
        createDisabled={backupDialogDomain === null || creatingBackupDomainId !== null}
        createBusy={backupDialogCreating}
        createDone={backupDialogCreated}
        onRestoreBackup={(name) => {
          void handleRestoreBackup(name);
        }}
        restoringBackupName={restoringBackupName}
        restoredBackupName={restoredBackupName}
        restoreConfirmTitle="Restore backup"
        restoreConfirmText="Restore backup"
        getRestoreConfirmDescription={(name) =>
          `Restore backup "${name}"? This overwrites the site files stored in that archive.`
        }
        onDeleteBackup={(name) => {
          void handleDeleteBackup(name);
        }}
        deletingBackupName={deletingBackupName}
        actionIconStroke={domainActionIconStroke}
      />
      <ActionConfirmDialog
        open={deleteDomainCandidate !== null}
        onOpenChange={(open) => {
          if (!open && deletingDomainId === null) {
            setDeleteDomainCandidate(null);
          }
        }}
        title="Delete domain"
        desc={
          deleteDomainCandidate
            ? `Delete ${deleteDomainCandidate.hostname}? This removes it from FlowPanel and republishes the active routing.`
            : "Delete this domain?"
        }
        confirmText="Delete domain"
        destructive
        isLoading={deleteDomainCandidate !== null && deletingDomainId === deleteDomainCandidate.id}
        handleConfirm={() => {
          void confirmDeleteDomain();
        }}
        className="sm:max-w-md"
      />

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
              <div className="border-b border-[var(--app-border)] px-6 py-4">
                <div className="text-[14px] font-medium text-[var(--app-text)]">
                  Domain list
                </div>
              </div>

              {loading ? (
                <div className="px-6 py-10 text-[13px] text-[var(--app-text-muted)]">
                  Loading domains...
                </div>
              ) : domains.length ? (
                <div className="px-6">
                  <Table>
                    <TableHeader>
                      <TableRow className="hover:bg-transparent">
                        <TableHead>Domain</TableHead>
                        <TableHead>Type</TableHead>
                        <TableHead>Backup</TableHead>
                        <TableHead className="w-[220px] text-right">Actions</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {domains.map((domain) => {
                        const filesPath = getFilesPathFromDomainTarget(
                          domain.kind,
                          sitesBasePath,
                          domain.target,
                        );
                        const backupCount = siteBackups[domain.hostname]?.length ?? 0;

                        return (
                          <TableRow key={domain.id}>
                            <TableCell className="font-medium text-[var(--app-text)]">
                              <div className="flex flex-wrap items-center gap-2">
                                <Link
                                  to="/domains/$hostname"
                                  params={{ hostname: domain.hostname }}
                                  className="transition-colors hover:text-primary hover:underline"
                                >
                                  {domain.hostname}
                                </Link>
                                <Badge asChild variant="outline" className="rounded-full">
                                  <a
                                    href={getDomainSiteUrl(domain.hostname)}
                                    target="_blank"
                                    rel="noreferrer"
                                    aria-label={`Visit ${domain.hostname}`}
                                    title={`Visit ${domain.hostname}`}
                                  >
                                    <ExternalLink className="h-3 w-3" />
                                    Visit
                                  </a>
                                </Badge>
                              </div>
                            </TableCell>
                            <TableCell>{domain.kind}</TableCell>
                            <TableCell>
                              {!isSiteBackedKind(domain.kind) ? (
                                <span className="text-[13px] text-[var(--app-text-muted)]">
                                  Not available
                                </span>
                              ) : backupsLoading ? (
                                <span className="text-[13px] text-[var(--app-text-muted)]">
                                  Loading...
                                </span>
                              ) : backupsLoadError ? (
                                <span
                                  className="text-[13px] text-[var(--app-text-muted)]"
                                  title={backupsLoadError}
                                >
                                  Unavailable
                                </span>
                              ) : (
                                <button
                                  type="button"
                                  onClick={() => setBackupDialogDomain(domain)}
                                  className={cn(
                                    "text-[13px] font-medium underline decoration-[var(--app-border-strong)] underline-offset-4 transition",
                                    backupCount > 0
                                      ? "text-[var(--app-text)] hover:text-[var(--app-text-muted)]"
                                      : "text-[var(--app-text-muted)] hover:text-[var(--app-text)]",
                                  )}
                                >
                                  {backupCount > 0
                                    ? `${backupCount} ${backupCount === 1 ? "backup" : "backups"}`
                                    : "No backups"}
                                </button>
                              )}
                            </TableCell>
                            <TableCell className="w-[220px]">
                              <div className="flex items-center justify-end gap-0.5">
                                {filesPath !== null ? (
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon"
                                    onClick={() => {
                                      void handleDownload(domain, filesPath);
                                    }}
                                    disabled={downloadingDomainId !== null}
                                    aria-label={`Download files for ${domain.hostname}`}
                                    title={
                                      downloadingDomainId === domain.id
                                        ? `Downloading files for ${domain.hostname}`
                                        : `Download files for ${domain.hostname}`
                                    }
                                    className={domainActionButtonClass}
                                  >
                                    {downloadingDomainId === domain.id ? (
                                      <LoaderCircle
                                        className="size-6 animate-spin"
                                        stroke={domainActionIconStroke}
                                      />
                                    ) : (
                                      <Download
                                        className="size-6"
                                        stroke={domainActionIconStroke}
                                      />
                                    )}
                                  </Button>
                                ) : null}
                                {filesPath !== null ? (
                                  <Button
                                    asChild
                                    variant="ghost"
                                    size="icon"
                                    aria-label={`Open site folder for ${domain.hostname}`}
                                    title="Open site folder"
                                    className={domainActionButtonClass}
                                  >
                                    <Link
                                      to="/files"
                                      search={filesPath ? { path: filesPath } : {}}
                                    >
                                      <FolderOpen
                                        className="size-6"
                                        stroke={domainActionIconStroke}
                                      />
                                    </Link>
                                  </Button>
                                ) : null}
                                <Button
                                  type="button"
                                  variant="ghost"
                                  size="icon"
                                  onClick={() => openEditForm(domain)}
                                  disabled={deletingDomainId !== null}
                                  aria-label={`Edit ${domain.hostname}`}
                                  title="Edit"
                                  className={domainActionButtonClass}
                                >
                                  <Pencil
                                    className="size-6"
                                    stroke={domainActionIconStroke}
                                  />
                                </Button>
                                <Button
                                  type="button"
                                  variant="ghost"
                                  size="icon"
                                  onClick={() => {
                                    void handleDelete(domain);
                                  }}
                                  disabled={deletingDomainId !== null}
                                  className={domainDangerActionButtonClass}
                                  aria-label={`Delete ${domain.hostname}`}
                                  title="Delete"
                                >
                                  {deletingDomainId === domain.id ? (
                                    <LoaderCircle
                                      className="size-6 animate-spin"
                                      stroke={domainActionIconStroke}
                                    />
                                  ) : (
                                    <Trash2
                                      className="size-6"
                                      stroke={domainActionIconStroke}
                                    />
                                  )}
                                </Button>
                              </div>
                            </TableCell>
                          </TableRow>
                        );
                      })}
                    </TableBody>
                  </Table>
                </div>
              ) : (
                <div className="px-6 py-10">
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
                  const nextHostname = event.target.value;
                  const normalizedHostname = normalizeHostname(nextHostname);

                  setForm((current) => ({
                    ...current,
                    hostname: nextHostname,
                  }));
                  setErrors((current) => ({
                    ...current,
                    hostname: getDuplicateHostnameError(
                      normalizedHostname,
                      domains,
                      editingDomainId,
                    ),
                  }));
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
    </>
  );
}
