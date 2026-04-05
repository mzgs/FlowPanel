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
  deployDomainGitHubIntegration,
  fetchDomainPreview,
  fetchDomains,
  getDomainSiteUrl,
  updateDomainPHPSettings,
  type DomainApiError,
  type DomainRecord,
  updateDomainGitHubIntegration,
} from "@/api/domains";
import { fetchFileContent } from "@/api/files";
import {
  fetchMariaDBDatabases,
  type MariaDBDatabase,
} from "@/api/mariadb";
import {
  fetchPHPStatus,
  installPHP,
  startPHP,
  type PHPSettings,
  type PHPStatus,
} from "@/api/php";
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
import {
  DomainComposerDialog,
  type ComposerPackage,
} from "@/components/domain-composer-dialog";
import { DomainGitHubDialog } from "@/components/domain-github-dialog";
import { DomainPHPDialog } from "@/components/domain-php-dialog";
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

type GitHubFormState = {
  repositoryUrl: string;
  autoDeployOnPush: boolean;
};

const initialGitHubForm: GitHubFormState = {
  repositoryUrl: "",
  autoDeployOnPush: false,
};

function toGitHubFormState(domain: DomainRecord | null): GitHubFormState {
  if (!domain?.github_integration) {
    return initialGitHubForm;
  }

  return {
    repositoryUrl: domain.github_integration.repository_url,
    autoDeployOnPush: domain.github_integration.auto_deploy_on_push,
  };
}

function sameGitHubFormState(left: GitHubFormState, right: GitHubFormState) {
  return (
    left.repositoryUrl === right.repositoryUrl &&
    left.autoDeployOnPush === right.autoDeployOnPush
  );
}

const initialPHPSettings: PHPSettings = {
  max_execution_time: "",
  max_input_time: "",
  memory_limit: "",
  post_max_size: "",
  file_uploads: "On",
  upload_max_filesize: "",
  max_file_uploads: "",
  default_socket_timeout: "",
  error_reporting: "",
  display_errors: "Off",
};

function toPHPSettingsForm(
  status: PHPStatus | null,
  overrides?: PHPSettings | null,
): PHPSettings {
  const statusSettings = status?.settings ?? {};
  const base = status
    ? {
        max_execution_time: statusSettings.max_execution_time ?? "",
        max_input_time: statusSettings.max_input_time ?? "",
        memory_limit: statusSettings.memory_limit ?? "",
        post_max_size: statusSettings.post_max_size ?? "",
        file_uploads: statusSettings.file_uploads ?? "On",
        upload_max_filesize: statusSettings.upload_max_filesize ?? "",
        max_file_uploads: statusSettings.max_file_uploads ?? "",
        default_socket_timeout: statusSettings.default_socket_timeout ?? "",
        error_reporting: statusSettings.error_reporting ?? "",
        display_errors: statusSettings.display_errors ?? "Off",
      }
    : initialPHPSettings;

  if (!overrides) {
    return base;
  }

  return {
    max_execution_time: overrides.max_execution_time || base.max_execution_time,
    max_input_time: overrides.max_input_time || base.max_input_time,
    memory_limit: overrides.memory_limit || base.memory_limit,
    post_max_size: overrides.post_max_size || base.post_max_size,
    file_uploads: overrides.file_uploads || base.file_uploads,
    upload_max_filesize: overrides.upload_max_filesize || base.upload_max_filesize,
    max_file_uploads: overrides.max_file_uploads || base.max_file_uploads,
    default_socket_timeout:
      overrides.default_socket_timeout || base.default_socket_timeout,
    error_reporting: overrides.error_reporting || base.error_reporting,
    display_errors: overrides.display_errors || base.display_errors,
  };
}

function samePHPSettings(left: PHPSettings, right: PHPSettings) {
  return (
    left.max_execution_time === right.max_execution_time &&
    left.max_input_time === right.max_input_time &&
    left.memory_limit === right.memory_limit &&
    left.post_max_size === right.post_max_size &&
    left.file_uploads === right.file_uploads &&
    left.upload_max_filesize === right.upload_max_filesize &&
    left.max_file_uploads === right.max_file_uploads &&
    left.default_socket_timeout === right.default_socket_timeout &&
    left.error_reporting === right.error_reporting &&
    left.display_errors === right.display_errors
  );
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
    title: "Github",
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
const composerManifestName = "composer.json";

function isSiteBackedKind(kind: DomainRecord["kind"]) {
  return kind === "Static site" || kind === "Php site";
}

function getComposerManifestPath(path: string | null) {
  if (path === null) {
    return null;
  }

  return path ? `${path}/${composerManifestName}` : composerManifestName;
}

function parseComposerPackages(content: string) {
  const payload = JSON.parse(content) as {
    require?: Record<string, string>;
    "require-dev"?: Record<string, string>;
  };
  const packages: ComposerPackage[] = [];

  for (const [name, version] of Object.entries(payload.require ?? {})) {
    packages.push({ name, version, dev: false });
  }

  for (const [name, version] of Object.entries(payload["require-dev"] ?? {})) {
    packages.push({ name, version, dev: true });
  }

  return packages.sort((left, right) => {
    if (left.name === right.name) {
      return Number(left.dev) - Number(right.dev);
    }

    return left.name.localeCompare(right.name);
  });
}

async function runComposerAction(hostname: string, action: "install" | "update") {
  const response = await fetch(`/api/domains/${encodeURIComponent(hostname)}/composer/${action}`, {
    method: "POST",
    credentials: "include",
  });

  if (response.ok) {
    return;
  }

  let message = `composer ${action} request failed with status ${response.status}`;

  try {
    const payload = (await response.json()) as { error?: unknown };
    if (typeof payload.error === "string" && payload.error) {
      message = payload.error;
    }
  } catch {
    // Ignore non-JSON error responses.
  }

  throw new Error(message);
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
  const [composerDialogOpen, setComposerDialogOpen] = useState(false);
  const [githubDialogOpen, setGitHubDialogOpen] = useState(false);
  const [phpDialogOpen, setPHPDialogOpen] = useState(false);
  const [composerHasManifest, setComposerHasManifest] = useState(false);
  const [composerPackages, setComposerPackages] = useState<ComposerPackage[]>([]);
  const [composerLoading, setComposerLoading] = useState(false);
  const [composerError, setComposerError] = useState<string | null>(null);
  const [composerRunningAction, setComposerRunningAction] = useState<
    "install" | "update" | null
  >(null);
  const [githubForm, setGitHubForm] = useState<GitHubFormState>(initialGitHubForm);
  const [savedGitHubForm, setSavedGitHubForm] = useState<GitHubFormState>(initialGitHubForm);
  const [githubSaving, setGitHubSaving] = useState(false);
  const [githubDeploying, setGitHubDeploying] = useState(false);
  const [githubError, setGitHubError] = useState<string | null>(null);
  const [githubFeedback, setGitHubFeedback] = useState<string | null>(null);
  const [githubFieldErrors, setGitHubFieldErrors] = useState<Record<string, string>>(
    {},
  );
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
  const [phpStatus, setPHPStatus] = useState<PHPStatus | null>(null);
  const [phpForm, setPHPForm] = useState<PHPSettings>(initialPHPSettings);
  const [savedPHPForm, setSavedPHPForm] = useState<PHPSettings>(initialPHPSettings);
  const [phpFieldErrors, setPHPFieldErrors] = useState<Record<string, string>>({});
  const [phpLoading, setPHPLoading] = useState(false);
  const [phpSaving, setPHPSaving] = useState(false);
  const [phpError, setPHPError] = useState<string | null>(null);
  const [phpRunningAction, setPHPRunningAction] = useState<
    "install" | "start" | "refresh" | null
  >(null);
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
    setComposerDialogOpen(false);
    setGitHubDialogOpen(false);
    setPHPDialogOpen(false);
    setComposerHasManifest(false);
    setComposerPackages([]);
    setComposerLoading(false);
    setComposerError(null);
    setComposerRunningAction(null);
    setGitHubForm(initialGitHubForm);
    setSavedGitHubForm(initialGitHubForm);
    setGitHubSaving(false);
    setGitHubDeploying(false);
    setGitHubError(null);
    setGitHubFeedback(null);
    setGitHubFieldErrors({});
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
    setPHPStatus(null);
    setPHPForm(initialPHPSettings);
    setSavedPHPForm(initialPHPSettings);
    setPHPFieldErrors({});
    setPHPLoading(false);
    setPHPSaving(false);
    setPHPError(null);
    setPHPRunningAction(null);

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
        const nextGitHubForm = toGitHubFormState(matchedDomain);

        setSitesBasePath(domainsResult.value.sites_base_path);
        setDomain(matchedDomain);
        setGitHubForm(nextGitHubForm);
        setSavedGitHubForm(nextGitHubForm);
        setGitHubError(null);
        setGitHubFieldErrors({});
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

  useEffect(() => {
    if (!phpDialogOpen || domain?.kind !== "Php site") {
      return;
    }

    let active = true;
    setPHPLoading(true);
    setPHPError(null);

    async function loadPHPStatus() {
      try {
        const nextStatus = await fetchPHPStatus();
        if (!active) {
          return;
        }
        setPHPStatus(nextStatus);
        const nextForm = toPHPSettingsForm(nextStatus, domain?.php_settings);
        setPHPForm(nextForm);
        setSavedPHPForm(nextForm);
        setPHPFieldErrors({});
      } catch (error) {
        if (!active) {
          return;
        }
        setPHPError(getErrorMessage(error, "Failed to load PHP settings."));
      } finally {
        if (active) {
          setPHPLoading(false);
        }
      }
    }

    void loadPHPStatus();

    return () => {
      active = false;
    };
  }, [domain?.hostname, domain?.kind, domain?.php_settings, phpDialogOpen]);

  const filesPath = domain
    ? getFilesPathFromDomainTarget(domain.kind, sitesBasePath, domain.target)
    : null;
  const composerManifestPath = getComposerManifestPath(filesPath);

  useEffect(() => {
    if (!composerDialogOpen || !domain || composerManifestPath === null) {
      return;
    }

    if (domain.kind !== "Static site" && domain.kind !== "Php site") {
      return;
    }

    let active = true;
    setComposerLoading(true);
    setComposerError(null);

    async function loadComposer() {
      try {
        const file = await fetchFileContent(composerManifestPath);
        if (!active) {
          return;
        }

        setComposerHasManifest(true);
        setComposerPackages(parseComposerPackages(file.content));
      } catch (error) {
        if (!active) {
          return;
        }

        const message = getErrorMessage(error, "Failed to load Composer details.");
        setComposerHasManifest(false);
        setComposerPackages([]);
        setComposerError(message === "file or directory not found" ? null : message);
      } finally {
        if (active) {
          setComposerLoading(false);
        }
      }
    }

    void loadComposer();

    return () => {
      active = false;
    };
  }, [composerDialogOpen, composerManifestPath, domain]);

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
  const githubDirty = !sameGitHubFormState(githubForm, savedGitHubForm);
  const phpDirty = !samePHPSettings(phpForm, savedPHPForm);

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

  async function handleComposerAction(action: "install" | "update") {
    if (!domain || composerManifestPath === null || composerRunningAction !== null) {
      return;
    }

    setComposerRunningAction(action);
    setComposerError(null);

    try {
      await runComposerAction(domain.hostname, action);
      const file = await fetchFileContent(composerManifestPath);
      setComposerHasManifest(true);
      setComposerPackages(parseComposerPackages(file.content));
      toast.success(`composer ${action} finished for ${domain.hostname}.`);
    } catch (error) {
      const message = getErrorMessage(error, `Failed to run composer ${action}.`);
      setComposerError(message);
      toast.error(message);
    } finally {
      setComposerRunningAction(null);
    }
  }

  async function saveGitHubIntegration(nextForm = githubForm) {
    if (!domain) {
      return null;
    }

    setGitHubSaving(true);
    setGitHubError(null);
    setGitHubFeedback(null);
    setGitHubFieldErrors({});

    try {
      const updatedDomain = await updateDomainGitHubIntegration(domain.hostname, {
        repository_url: nextForm.repositoryUrl.trim(),
        auto_deploy_on_push: nextForm.autoDeployOnPush,
      });
      const nextGitHubForm = toGitHubFormState(updatedDomain);
      setDomain(updatedDomain);
      setGitHubForm(nextGitHubForm);
      setSavedGitHubForm(nextGitHubForm);
      toast.success(
        nextGitHubForm.repositoryUrl
          ? `GitHub integration updated for ${updatedDomain.hostname}.`
          : `GitHub integration removed from ${updatedDomain.hostname}.`,
      );
      return updatedDomain;
    } catch (error) {
      const message = getErrorMessage(error, "Failed to save GitHub integration.");
      setGitHubError(message);
      setGitHubFieldErrors(
        typeof error === "object" && error !== null && "fieldErrors" in error
          ? ((error as { fieldErrors?: Record<string, string> }).fieldErrors ?? {})
          : {},
      );
      toast.error(message);
      return null;
    } finally {
      setGitHubSaving(false);
    }
  }

  async function handleDeployFromGitHub() {
    if (!domain || githubDeploying) {
      return;
    }

    setGitHubDeploying(true);
    setGitHubError(null);
    setGitHubFeedback(null);

    try {
      let activeDomain = domain;
      if (githubDirty || !domain.github_integration) {
        const savedDomain = await saveGitHubIntegration(githubForm);
        if (!savedDomain?.github_integration) {
          return;
        }
        activeDomain = savedDomain;
      }

      const result = await deployDomainGitHubIntegration(activeDomain.hostname);
      setPreviewRefreshing(true);
      setPreviewError(false);
      setPreviewErrorMessage(null);
      setPreviewRefreshToken(Date.now());
      const feedback =
        result.action === "updated"
          ? `Updated the local repository for ${activeDomain.hostname}.`
          : `Initialized the local repository for ${activeDomain.hostname}.`;
      setGitHubFeedback(feedback);
      toast.success(feedback);
    } catch (error) {
      const message = getErrorMessage(error, "Failed to deploy from GitHub.");
      setGitHubError(message);
      toast.error(message);
    } finally {
      setGitHubDeploying(false);
    }
  }

  async function handleDisconnectGitHub() {
    if (!domain || githubSaving) {
      return;
    }

    const nextForm = initialGitHubForm;
    setGitHubForm(nextForm);
    setGitHubFieldErrors({});
    await saveGitHubIntegration(nextForm);
  }

  async function refreshPHPSettings() {
    setPHPRunningAction("refresh");
    setPHPError(null);

    try {
      const nextStatus = await fetchPHPStatus();
      setPHPStatus(nextStatus);
      const nextForm = toPHPSettingsForm(nextStatus, domain?.php_settings);
      setPHPForm(nextForm);
      setSavedPHPForm(nextForm);
      setPHPFieldErrors({});
    } catch (error) {
      const message = getErrorMessage(error, "Failed to refresh PHP settings.");
      setPHPError(message);
      toast.error(message);
    } finally {
      setPHPRunningAction(null);
    }
  }

  async function handleInstallPHP() {
    setPHPRunningAction("install");
    setPHPError(null);

    try {
      const nextStatus = await installPHP();
      setPHPStatus(nextStatus);
      const nextForm = toPHPSettingsForm(nextStatus, domain?.php_settings);
      setPHPForm(nextForm);
      setSavedPHPForm(nextForm);
      setPHPFieldErrors({});
      toast.success("PHP installed.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install PHP.");
      setPHPError(message);
      toast.error(message);
    } finally {
      setPHPRunningAction(null);
    }
  }

  async function handleStartPHP() {
    setPHPRunningAction("start");
    setPHPError(null);

    try {
      const nextStatus = await startPHP();
      setPHPStatus(nextStatus);
      const nextForm = toPHPSettingsForm(nextStatus, domain?.php_settings);
      setPHPForm(nextForm);
      setSavedPHPForm(nextForm);
      setPHPFieldErrors({});
      toast.success("PHP-FPM started.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to start PHP-FPM.");
      setPHPError(message);
      toast.error(message);
    } finally {
      setPHPRunningAction(null);
    }
  }

  async function handleSavePHPSettings() {
    if (!domain) {
      return;
    }

    setPHPSaving(true);
    setPHPError(null);
    setPHPFieldErrors({});

    try {
      const updatedDomain = await updateDomainPHPSettings(domain.hostname, {
        max_execution_time: phpForm.max_execution_time ?? "",
        max_input_time: phpForm.max_input_time ?? "",
        memory_limit: phpForm.memory_limit ?? "",
        post_max_size: phpForm.post_max_size ?? "",
        file_uploads: phpForm.file_uploads ?? "On",
        upload_max_filesize: phpForm.upload_max_filesize ?? "",
        max_file_uploads: phpForm.max_file_uploads ?? "",
        default_socket_timeout: phpForm.default_socket_timeout ?? "",
        error_reporting: phpForm.error_reporting ?? "",
        display_errors: phpForm.display_errors ?? "Off",
      });
      const nextForm = toPHPSettingsForm(phpStatus, updatedDomain.php_settings);
      setDomain(updatedDomain);
      setPHPForm(nextForm);
      setSavedPHPForm(nextForm);
      toast.success("PHP settings saved.");
    } catch (error) {
      const phpSettingsError = error as DomainApiError;
      setPHPFieldErrors(phpSettingsError.fieldErrors ?? {});
      const message = phpSettingsError.message || "PHP settings could not be saved.";
      setPHPError(message);
      toast.error(message);
    } finally {
      setPHPSaving(false);
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
      <DomainComposerDialog
        open={composerDialogOpen && domain !== null}
        onOpenChange={setComposerDialogOpen}
        hostname={domain?.hostname ?? hostname}
        projectPath={domain?.target ?? ""}
        hasManifest={composerHasManifest}
        packages={composerPackages}
        loading={composerLoading}
        loadError={composerError}
        runningAction={composerRunningAction}
        onInstall={() => {
          void handleComposerAction("install");
        }}
        onUpdate={() => {
          void handleComposerAction("update");
        }}
      />
      <DomainGitHubDialog
        open={githubDialogOpen && domain !== null}
        onOpenChange={setGitHubDialogOpen}
        hostname={domain?.hostname ?? hostname}
        repositoryUrl={githubForm.repositoryUrl}
        autoDeployOnPush={githubForm.autoDeployOnPush}
        hasSavedIntegration={Boolean(domain?.github_integration)}
        saving={githubSaving}
        deploying={githubDeploying}
        fieldErrors={githubFieldErrors}
        error={githubError}
        feedback={githubFeedback}
        dirty={githubDirty}
        onRepositoryUrlChange={(value) => {
          setGitHubError(null);
          setGitHubFeedback(null);
          setGitHubFieldErrors((current) => {
            const next = { ...current };
            delete next.repository_url;
            return next;
          });
          setGitHubForm((current) => ({
            ...current,
            repositoryUrl: value,
          }));
        }}
        onAutoDeployOnPushChange={(checked) => {
          setGitHubError(null);
          setGitHubFeedback(null);
          setGitHubForm((current) => ({
            ...current,
            autoDeployOnPush: checked,
          }));
        }}
        onSave={() => {
          void saveGitHubIntegration(githubForm);
        }}
        onDeploy={() => {
          void handleDeployFromGitHub();
        }}
        onDisconnect={() => {
          void handleDisconnectGitHub();
        }}
      />
      {domain ? (
        <DomainPHPDialog
          open={phpDialogOpen && domain.kind === "Php site"}
          onOpenChange={setPHPDialogOpen}
          domain={domain}
          status={phpStatus}
          form={phpForm}
          fieldErrors={phpFieldErrors}
          loading={phpLoading}
          saving={phpSaving}
          error={phpError}
          dirty={phpDirty}
          runningAction={phpRunningAction}
          onFieldChange={(field, value) => {
            setPHPError(null);
            setPHPFieldErrors((current) => {
              const next = { ...current };
              delete next[field];
              return next;
            });
            setPHPForm((current) => ({
              ...current,
              [field]: value,
            }));
          }}
          onRefresh={() => {
            void refreshPHPSettings();
          }}
          onInstall={() => {
            void handleInstallPHP();
          }}
          onStart={() => {
            void handleStartPHP();
          }}
          onSave={() => {
            void handleSavePHPSettings();
          }}
        />
      ) : null}
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
                      if (item.title === "PHP" && domain !== null) {
                        if (domain.kind !== "Php site") {
                          toast.error("PHP settings are available only for PHP site domains.");
                          return;
                        }

                        setPHPDialogOpen(true);
                        return;
                      }

                      if (item.title === "PHP Composer" && domain !== null) {
                        if (domain.kind !== "Static site" && domain.kind !== "Php site") {
                          toast.error(
                            "Composer is available only for Static site and Php site domains.",
                          );
                          return;
                        }

                        setComposerDialogOpen(true);
                        return;
                      }

                      if (item.title === "Logs" && domain !== null) {
                        void navigate({
                          to: "/domains/$hostname/logs",
                          params: { hostname: domain.hostname },
                        });
                        return;
                      }

                      if (item.title === "Github" && domain !== null) {
                        if (domain.kind !== "Static site" && domain.kind !== "Php site") {
                          toast.error(
                            "GitHub integration is available only for Static site and Php site domains.",
                          );
                          return;
                        }

                        setGitHubDialogOpen(true);
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
