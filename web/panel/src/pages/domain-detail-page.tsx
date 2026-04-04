import { useNavigate, useParams } from "@tanstack/react-router";
import { useEffect, useRef, useState, type ComponentType } from "react";
import {
  createBackup,
  deleteBackup,
  fetchBackups,
  restoreBackup,
  type BackupRecord,
} from "@/api/backups";
import {
  fetchDomainPreview,
  fetchDomains,
  getDomainSiteUrl,
  type DomainRecord,
} from "@/api/domains";
import {
  fetchMariaDBDatabases,
  type MariaDBDatabase,
} from "@/api/mariadb";
import {
  Clock,
  Copy,
  Database,
  Download,
  ExternalLink,
  File,
  FileCode2,
  Folder,
  FolderOpen,
  GitBranch,
  Globe,
  HardDrive,
  LoaderCircle,
  Monitor,
  Package,
  RefreshCw,
  Settings2,
  Sparkles,
  Telescope,
  TerminalSquare,
} from "@/components/icons/tabler-icons";
import { DomainBackupRestoreDialog } from "@/components/domain-backup-restore-dialog";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import {
  getDatabaseNameFromBackupRecord,
  getSiteHostnameFromBackupRecord,
} from "@/lib/backup-records";
import { getFilesPathFromDomainTarget } from "@/lib/domain-targets";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

type ActionIcon = ComponentType<{
  className?: string;
  size?: number | string;
  stroke?: number | string;
}>;

type DomainActionItem = {
  title: string;
  icon: ActionIcon;
};

const fileAndDatabaseActions: DomainActionItem[] = [
  {
    title: "Connection Info",
    icon: Globe,
  },
  {
    title: "Files",
    icon: Folder,
  },
  {
    title: "Databases",
    icon: Database,
  },
  {
    title: "FTP",
    icon: FolderOpen,
  },
  {
    title: "Backup & Restore",
    icon: HardDrive,
  },
  {
    title: "Website Copying",
    icon: Copy,
  },
];

const devToolActions: DomainActionItem[] = [
  {
    title: "PHP",
    icon: FileCode2,
  },
  {
    title: "Logs",
    icon: File,
  },
  {
    title: "SSH Terminal",
    icon: TerminalSquare,
  },
  {
    title: "Monitoring",
    icon: Monitor,
  },
  {
    title: "PHP Composer",
    icon: Package,
  },
  {
    title: "Scheduled Tasks",
    icon: Clock,
  },
  {
    title: "Performance Booster",
    icon: Sparkles,
  },
  {
    title: "Git",
    icon: GitBranch,
  },
  {
    title: "SEO",
    icon: Telescope,
  },
  {
    title: "Website Importing",
    icon: Download,
  },
  {
    title: "Docker Proxy Rules",
    icon: Settings2,
  },
];

const siteBackupTargetKey = "__domain_site_backup__";

function isSiteBackedKind(kind: DomainRecord["kind"]) {
  return kind === "Static site" || kind === "Php site";
}

function DomainActionSection({
  title,
  items,
  onItemClick,
}: {
  title: string;
  items: DomainActionItem[];
  onItemClick?: (item: DomainActionItem) => void;
}) {
  return (
    <section className="space-y-2">
      <h2 className="pl-2 text-base font-semibold text-[var(--app-text)]">{title}</h2>
      <div className="grid gap-x-3 gap-y-1.5 md:grid-cols-2 xl:grid-cols-3">
        {items.map(({ title: itemTitle, icon: Icon }) => (
          <button
            key={itemTitle}
            type="button"
            onClick={() => onItemClick?.({ title: itemTitle, icon: Icon })}
            className="group flex items-center gap-3 rounded-lg px-2 py-1 text-left transition-colors duration-150 hover:bg-[var(--app-surface-muted)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--app-accent)]"
          >
            <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] text-[var(--app-text-muted)] transition-colors duration-150 group-hover:text-[var(--app-accent)]">
              <Icon className="h-5 w-5" stroke={1.75} />
            </span>
            <span className="min-w-0 text-sm font-medium leading-5 text-[var(--app-text)]">
              {itemTitle}
            </span>
          </button>
        ))}
      </div>
    </section>
  );
}

export function DomainDetailPage() {
  const { hostname } = useParams({ from: "/domains/$hostname" });
  const navigate = useNavigate();
  const [domain, setDomain] = useState<DomainRecord | null>(null);
  const [sitesBasePath, setSitesBasePath] = useState("");
  const [backups, setBackups] = useState<BackupRecord[]>([]);
  const [databases, setDatabases] = useState<MariaDBDatabase[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [backupDataLoading, setBackupDataLoading] = useState(true);
  const [backupDataError, setBackupDataError] = useState<string | null>(null);
  const [backupDialogOpen, setBackupDialogOpen] = useState(false);
  const [creatingBackupTarget, setCreatingBackupTarget] = useState<string | null>(
    null,
  );
  const [createdBackupTarget, setCreatedBackupTarget] = useState<string | null>(null);
  const [restoringBackupName, setRestoringBackupName] = useState<string | null>(
    null,
  );
  const [restoredBackupName, setRestoredBackupName] = useState<string | null>(null);
  const [deletingBackupName, setDeletingBackupName] = useState<string | null>(null);
  const [previewUrl, setPreviewUrl] = useState("");
  const [previewLoaded, setPreviewLoaded] = useState(false);
  const [previewError, setPreviewError] = useState(false);
  const [previewErrorMessage, setPreviewErrorMessage] = useState<string | null>(
    null,
  );
  const [previewRefreshing, setPreviewRefreshing] = useState(false);
  const [previewRefreshToken, setPreviewRefreshToken] = useState(0);
  const createdBackupTimeoutRef = useRef<number | null>(null);
  const restoredBackupTimeoutRef = useRef<number | null>(null);
  const siteUrl = domain ? getDomainSiteUrl(domain.hostname) : "";

  useEffect(() => {
    let active = true;
    setLoading(true);
    setLoadError(null);
    setDomain(null);
    setSitesBasePath("");
    setBackups([]);
    setDatabases([]);
    setBackupDataLoading(true);
    setBackupDataError(null);
    setBackupDialogOpen(false);
    setCreatingBackupTarget(null);
    setCreatedBackupTarget(null);
    setRestoringBackupName(null);
    setRestoredBackupName(null);
    setDeletingBackupName(null);
    setPreviewUrl("");
    setPreviewLoaded(false);
    setPreviewError(false);
    setPreviewErrorMessage(null);
    setPreviewRefreshing(false);
    setPreviewRefreshToken(0);

    async function loadDomain() {
      const [domainsResult, backupsResult, databasesResult] = await Promise.allSettled([
        fetchDomains(),
        fetchBackups(),
        fetchMariaDBDatabases(),
      ]);
      if (!active) {
        return;
      }

      if (domainsResult.status === "fulfilled") {
        const matchedDomain =
          domainsResult.value.domains.find((record) => record.hostname === hostname) ??
          null;

        setSitesBasePath(domainsResult.value.sites_base_path);
        setDomain(matchedDomain);
        setLoadError(matchedDomain ? null : "The selected domain could not be found.");
      } else {
        setLoadError(getErrorMessage(domainsResult.reason, "Failed to load domain details."));
      }

      const backupErrors: string[] = [];

      if (backupsResult.status === "fulfilled") {
        setBackups(backupsResult.value.backups);
      } else {
        setBackups([]);
        backupErrors.push(
          getErrorMessage(backupsResult.reason, "Failed to load backups."),
        );
      }

      if (databasesResult.status === "fulfilled") {
        setDatabases(databasesResult.value.databases);
      } else {
        setDatabases([]);
        backupErrors.push(
          getErrorMessage(
            databasesResult.reason,
            "Failed to load databases for backups.",
          ),
        );
      }

      setBackupDataError(backupErrors.length > 0 ? backupErrors.join(" ") : null);
      setLoading(false);
      setBackupDataLoading(false);
    }

    void loadDomain();

    return () => {
      active = false;
    };
  }, [hostname]);

  useEffect(() => {
    if (!previewUrl.startsWith("blob:")) {
      return;
    }

    return () => {
      URL.revokeObjectURL(previewUrl);
    };
  }, [previewUrl]);

  useEffect(() => {
    if (!domain) {
      return;
    }

    let active = true;
    const controller = new AbortController();
    const refreshRequested = previewRefreshToken > 0;

    if (!previewUrl) {
      setPreviewLoaded(false);
    }
    setPreviewError(false);
    setPreviewErrorMessage(null);
    setPreviewRefreshing(refreshRequested);

    async function loadPreview() {
      try {
        const blob = await fetchDomainPreview(domain.hostname, {
          refresh: refreshRequested,
          refreshToken: previewRefreshToken || undefined,
          signal: controller.signal,
        });
        if (!active) {
          return;
        }

        const objectUrl = URL.createObjectURL(blob);
        setPreviewUrl((currentUrl) => {
          if (currentUrl.startsWith("blob:")) {
            URL.revokeObjectURL(currentUrl);
          }

          return objectUrl;
        });
      } catch (error) {
        if (!active) {
          return;
        }

        setPreviewLoaded(false);
        setPreviewError(!previewUrl);
        setPreviewErrorMessage(getErrorMessage(error, "Preview is unavailable right now."));
        setPreviewRefreshing(false);
      }
    }

    void loadPreview();

    return () => {
      active = false;
      controller.abort();
    };
  }, [domain?.hostname, previewRefreshToken]);

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

  const filesPath = domain
    ? getFilesPathFromDomainTarget(domain.kind, sitesBasePath, domain.target)
    : null;
  const showSiteBackups = domain ? isSiteBackedKind(domain.kind) : false;
  const siteBackups = domain
    ? backups.filter(
        (backup) => getSiteHostnameFromBackupRecord(backup) === domain.hostname,
      )
    : [];
  const databaseBackups = backups.reduce<Record<string, BackupRecord[]>>(
    (groups, backup) => {
      const databaseName = getDatabaseNameFromBackupRecord(backup);
      if (!databaseName) {
        return groups;
      }

      if (!groups[databaseName]) {
        groups[databaseName] = [];
      }

      groups[databaseName].push(backup);
      return groups;
    },
    {},
  );
  const domainDatabases = domain
    ? databases.filter((database) => database.domain === domain.hostname)
    : [];
  const databaseSections = domainDatabases.map((database) => ({
    name: database.name,
    backups: databaseBackups[database.name] ?? [],
  }));

  async function handleCreateSiteBackup() {
    if (!domain || !showSiteBackups || creatingBackupTarget !== null) {
      return;
    }

    setCreatingBackupTarget(siteBackupTargetKey);
    setCreatedBackupTarget(null);

    try {
      const record = await createBackup({
        include_panel_data: false,
        include_sites: true,
        include_databases: false,
        site_hostnames: [domain.hostname],
      });
      setBackups((current) => [
        record,
        ...current.filter((item) => item.name !== record.name),
      ]);
      setBackupDataError(null);
      if (createdBackupTimeoutRef.current !== null) {
        window.clearTimeout(createdBackupTimeoutRef.current);
      }
      setCreatedBackupTarget(siteBackupTargetKey);
      createdBackupTimeoutRef.current = window.setTimeout(() => {
        setCreatedBackupTarget((current) =>
          current === siteBackupTargetKey ? null : current,
        );
        createdBackupTimeoutRef.current = null;
      }, 1500);
      toast.success(`Created backup ${record.name}.`);
    } catch (error) {
      toast.error(
        getErrorMessage(error, `Failed to create backup for ${domain.hostname}.`),
      );
    } finally {
      setCreatingBackupTarget(null);
    }
  }

  async function handleCreateDatabaseBackup(name: string) {
    if (creatingBackupTarget !== null) {
      return;
    }

    setCreatingBackupTarget(name);
    setCreatedBackupTarget(null);

    try {
      const record = await createBackup({
        include_panel_data: false,
        include_sites: false,
        include_databases: true,
        database_names: [name],
      });
      setBackups((current) => [
        record,
        ...current.filter((item) => item.name !== record.name),
      ]);
      setBackupDataError(null);
      if (createdBackupTimeoutRef.current !== null) {
        window.clearTimeout(createdBackupTimeoutRef.current);
      }
      setCreatedBackupTarget(name);
      createdBackupTimeoutRef.current = window.setTimeout(() => {
        setCreatedBackupTarget((current) => (current === name ? null : current));
        createdBackupTimeoutRef.current = null;
      }, 1500);
      toast.success(`Created local backup ${record.name}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, `Failed to create local backup for ${name}.`));
    } finally {
      setCreatingBackupTarget(null);
    }
  }

  async function handleRestoreBackup(name: string) {
    if (restoringBackupName === name || deletingBackupName === name) {
      return;
    }

    setRestoringBackupName(name);
    setRestoredBackupName(null);

    try {
      const result = await restoreBackup(name);
      if (restoredBackupTimeoutRef.current !== null) {
        window.clearTimeout(restoredBackupTimeoutRef.current);
      }
      setRestoredBackupName(name);
      restoredBackupTimeoutRef.current = window.setTimeout(() => {
        setRestoredBackupName((current) => (current === name ? null : current));
        restoredBackupTimeoutRef.current = null;
      }, 1500);

      if (
        domain &&
        result.restored_sites?.some((restoredHostname) => restoredHostname === domain.hostname)
      ) {
        setPreviewRefreshing(true);
        setPreviewError(false);
        setPreviewErrorMessage(null);
        setPreviewRefreshToken(Date.now());
      }
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
      <DomainBackupRestoreDialog
        open={backupDialogOpen && domain !== null}
        onOpenChange={setBackupDialogOpen}
        hostname={domain?.hostname ?? hostname}
        showSiteBackups={showSiteBackups}
        siteBackups={siteBackups}
        databaseSections={databaseSections}
        loading={backupDataLoading}
        loadError={backupDataError}
        onCreateSiteBackup={() => {
          void handleCreateSiteBackup();
        }}
        createSiteBackupDisabled={creatingBackupTarget !== null || !showSiteBackups}
        createSiteBackupBusy={creatingBackupTarget === siteBackupTargetKey}
        createSiteBackupDone={createdBackupTarget === siteBackupTargetKey}
        onCreateDatabaseBackup={(name) => {
          void handleCreateDatabaseBackup(name);
        }}
        createDatabaseBackupDisabled={creatingBackupTarget !== null}
        creatingDatabaseBackupName={creatingBackupTarget}
        createdDatabaseBackupName={
          createdBackupTarget === siteBackupTargetKey ? null : createdBackupTarget
        }
        onRestoreBackup={(name) => {
          void handleRestoreBackup(name);
        }}
        restoringBackupName={restoringBackupName}
        restoredBackupName={restoredBackupName}
        onDeleteBackup={(name) => {
          void handleDeleteBackup(name);
        }}
        deletingBackupName={deletingBackupName}
      />
      <PageHeader
        title={
          loading ? (
            "Domain details"
          ) : domain ? (
            <span className="flex flex-wrap items-center gap-3">
              <span>{domain.hostname}</span>
              <Badge asChild variant="outline" className="rounded-full align-middle">
                <a
                  href={siteUrl}
                  target="_blank"
                  rel="noreferrer"
                  aria-label={`Visit ${domain.hostname}`}
                  title={`Visit ${domain.hostname}`}
                >
                  <ExternalLink className="h-3 w-3" />
                  Visit
                </a>
              </Badge>
            </span>
          ) : (
            "Domain details"
          )
        }
        meta={
          loading
            ? "Loading domain details..."
            : domain
              ? "Files, databases, and developer tools for this domain."
              : "This route is reserved for per-domain configuration."
        }
      />

      <div className="px-4 pb-1 sm:px-6 lg:px-8">
        <div className="space-y-4">
          {loadError ? (
            <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
              {loadError}
            </section>
          ) : null}

          {!loadError ? (
            <section className="grid gap-4 xl:grid-cols-[280px_minmax(0,1fr)]">
              <aside className="space-y-3">
                <div className="w-[280px] max-w-full overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] shadow-[var(--app-shadow)]">
                  <div className="relative aspect-[4/3] w-full bg-[var(--app-surface-muted)]">
                    {domain ? (
                      <>
                        <a
                          href={siteUrl}
                          target="_blank"
                          rel="noreferrer"
                          aria-label={`Visit ${domain.hostname}`}
                          title={`Visit ${domain.hostname}`}
                          className="group block h-full w-full"
                        >
                          {previewUrl ? (
                            <img
                              src={previewUrl}
                              alt={`${domain.hostname} site preview`}
                              className={cn(
                                "h-full w-full object-contain transition-opacity duration-200",
                                previewLoaded ? "opacity-100" : "opacity-0",
                              )}
                              loading="eager"
                              onLoad={() => {
                                setPreviewLoaded(true);
                                setPreviewError(false);
                                setPreviewErrorMessage(null);
                                setPreviewRefreshing(false);
                              }}
                              onError={() => {
                                setPreviewLoaded(false);
                                setPreviewError(true);
                                setPreviewErrorMessage("Preview image could not be displayed.");
                                setPreviewRefreshing(false);
                              }}
                            />
                          ) : null}

                          {!previewLoaded && (!previewUrl || previewError) ? (
                            <div className="absolute inset-0 flex flex-col justify-between bg-[var(--app-surface)]/92 p-4">
                              <div className="inline-flex w-fit rounded-full border border-[var(--app-border)] bg-[var(--app-surface)]/85 px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--app-text-muted)]">
                                Preview
                              </div>
                              <div>
                                <p className="text-sm font-semibold text-[var(--app-text)]">
                                  {domain.hostname}
                                </p>
                                <p className="mt-1 text-xs text-[var(--app-text-muted)]">
                                  {previewError
                                    ? previewErrorMessage ?? "Preview is unavailable right now."
                                    : previewRefreshing
                                      ? "Refreshing preview..."
                                      : "Loading cached preview..."}
                                </p>
                              </div>
                            </div>
                          ) : null}
                        </a>

                        <button
                          type="button"
                          className="absolute right-3 bottom-3 z-10 inline-flex h-9 w-9 items-center justify-center rounded-full border border-[var(--app-border)] bg-[var(--app-surface)]/92 text-[var(--app-text)] shadow-[var(--app-shadow)] transition hover:bg-[var(--app-surface)] disabled:cursor-not-allowed disabled:opacity-70"
                          aria-label={`Refresh preview for ${domain.hostname}`}
                          title="Refresh preview"
                          disabled={previewRefreshing}
                          onClick={() => {
                            setPreviewRefreshing(true);
                            setPreviewError(false);
                            setPreviewErrorMessage(null);
                            setPreviewRefreshToken(Date.now());
                          }}
                        >
                          {previewRefreshing ? (
                            <LoaderCircle className="h-4 w-4 animate-spin" />
                          ) : (
                            <RefreshCw className="h-4 w-4" />
                          )}
                        </button>
                      </>
                    ) : (
                      <div className="flex h-full items-center justify-center text-sm text-[var(--app-text-muted)]">
                        Loading preview...
                      </div>
                    )}
                  </div>
                </div>
                {previewErrorMessage && previewUrl ? (
                  <p className="text-xs leading-5 text-[var(--app-text-muted)]">
                    {previewErrorMessage}
                  </p>
                ) : null}

                <section className="w-[280px] max-w-full rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] p-5 shadow-[var(--app-shadow)]">
                  <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--app-text-muted)]">
                    Overview
                  </p>
                  <dl className="mt-4 space-y-4 text-sm">
                    <div>
                      <dt className="text-[var(--app-text-muted)]">Hostname</dt>
                      <dd className="mt-1 font-medium text-[var(--app-text)]">
                        {domain?.hostname ?? "..."}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-[var(--app-text-muted)]">Type</dt>
                      <dd className="mt-1 font-medium text-[var(--app-text)]">
                        {domain?.kind ?? "..."}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-[var(--app-text-muted)]">Caching</dt>
                      <dd className="mt-1 font-medium text-[var(--app-text)]">
                        {domain ? (domain.cache_enabled ? "Enabled" : "Disabled") : "..."}
                      </dd>
                    </div>
                  </dl>
                </section>
              </aside>
              <div className="space-y-4">
                <DomainActionSection
                  title="Files & Databases"
                  items={fileAndDatabaseActions}
                  onItemClick={(item) => {
                    if (item.title === "Files" && filesPath !== null) {
                      void navigate({
                        to: "/files",
                        search: filesPath ? { path: filesPath } : {},
                      });
                      return;
                    }

                    if (item.title === "Databases" && domain !== null) {
                      void navigate({
                        to: "/database",
                        search: { domain: domain.hostname },
                      });
                      return;
                    }

                    if (item.title === "Backup & Restore" && domain !== null) {
                      setBackupDialogOpen(true);
                    }
                  }}
                />
                <div className="pt-2">
                  <DomainActionSection
                    title="Dev Tools"
                    items={devToolActions}
                    onItemClick={(item) => {
                      if (item.title === "Logs" && domain !== null) {
                        void navigate({
                          to: "/domains/$hostname/logs",
                          params: { hostname: domain.hostname },
                        });
                      }
                    }}
                  />
                </div>
              </div>
            </section>
          ) : null}
        </div>
      </div>
    </>
  );
}
