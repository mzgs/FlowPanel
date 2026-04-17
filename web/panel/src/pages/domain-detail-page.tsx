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
  fetchDomainWordPressStatus,
  fetchDomainWordPressSummary,
  runDomainWordPressPluginAction,
  runDomainWordPressThemeAction,
  type WordPressExtension,
  type WordPressSummary,
  type WordPressStatus,
} from "@/api/domain-wordpress";
import {
  copyDomainWebsite,
  deployDomainGitHubIntegration,
  fetchDomainNodeJSStatus,
  fetchDomainPreview,
  fetchDomains,
  getDomainSiteUrl,
  installDomainNodeJSPackages,
  startDomainNodeJS,
  stopDomainNodeJS,
  type DomainNodeJSStatus,
  type InstallDomainTemplateResult,
  updateDomainPHPSettings,
  type DomainApiError,
  type DomainRecord,
  updateDomainGitHubIntegration,
} from "@/api/domains";
import { fetchFileContent } from "@/api/files";
import { fetchMariaDBDatabases, type MariaDBDatabase } from "@/api/mariadb";
import { clearPM2ProcessLogs, fetchPM2ProcessLogs } from "@/api/pm2";
import {
  fetchPHPStatus,
  installPHP,
  startPHP,
  type PHPRuntimeStatus,
  type PHPSettings,
  type PHPStatus,
} from "@/api/php";
import {
  BrandWordpress,
  Copy,
  Database,
  Download,
  ExternalLink,
  File,
  FileCode2,
  FilePlus2,
  Folder,
  FolderOpen,
  GitBranch,
  Globe,
  HardDrive,
  LoaderCircle,
  Monitor,
  Package,
  PlayerPlay,
  PlayerStop,
  RefreshCw,
  TerminalSquare,
  Trash2,
} from "@/components/icons/tabler-icons";
import { DomainBackupRestoreDialog } from "@/components/domain-backup-restore-dialog";
import {
  DomainComposerDialog,
  type ComposerPackage,
} from "@/components/domain-composer-dialog";
import { DomainFTPDialog } from "@/components/domain-ftp-dialog";
import { DomainGitHubDialog } from "@/components/domain-github-dialog";
import { DomainPHPDialog } from "@/components/domain-php-dialog";
import { DomainTemplateInstallDialog } from "@/components/domain-template-install-dialog";
import { DomainWordPressExtensionInstallDialog } from "@/components/domain-wordpress-extension-install-dialog";
import { DomainWebsiteCopyDialog } from "@/components/domain-website-copy-dialog";
import { PageHeader } from "@/components/page-header";
import { TerminalWindow } from "@/components/terminal-window";
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
  getDatabaseNameFromBackupRecord,
  getSiteHostnameFromBackupRecord,
} from "@/lib/backup-records";
import {
  getDocumentRootDisplayPath,
  getFilesPathFromDomainTarget,
} from "@/lib/domain-targets";
import { setPendingFilesPath } from "@/lib/files-navigation";
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
  postFetchScript: string;
};

const initialGitHubForm: GitHubFormState = {
  repositoryUrl: "",
  autoDeployOnPush: false,
  postFetchScript: "",
};

const defaultPHPErrorReporting = "E_ALL & ~E_NOTICE & ~E_DEPRECATED";
const defaultDisabledFunctions =
  "exec,passthru,shell_exec,system,proc_open,popen,pcntl_exec";
const phpErrorReportingOptions = new Set([
  "E_ALL",
  "E_ALL & ~E_NOTICE",
  "E_ALL & ~E_DEPRECATED",
  defaultPHPErrorReporting,
]);

function normalizePHPErrorReporting(value?: string | null) {
  const normalized = value?.trim();
  if (!normalized || !phpErrorReportingOptions.has(normalized)) {
    return defaultPHPErrorReporting;
  }

  return normalized;
}

function toGitHubFormState(domain: DomainRecord | null): GitHubFormState {
  if (!domain?.github_integration) {
    return initialGitHubForm;
  }

  return {
    repositoryUrl: domain.github_integration.repository_url,
    autoDeployOnPush: domain.github_integration.auto_deploy_on_push,
    postFetchScript: domain.github_integration.post_fetch_script,
  };
}

function sameGitHubFormState(left: GitHubFormState, right: GitHubFormState) {
  return (
    left.repositoryUrl === right.repositoryUrl &&
    left.autoDeployOnPush === right.autoDeployOnPush &&
    left.postFetchScript === right.postFetchScript
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
  error_reporting: defaultPHPErrorReporting,
  display_errors: "Off",
  disable_functions: defaultDisabledFunctions,
};

function toPHPSettingsForm(
  status: PHPRuntimeStatus | null,
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
        error_reporting: normalizePHPErrorReporting(
          statusSettings.error_reporting,
        ),
        display_errors: statusSettings.display_errors ?? "Off",
        disable_functions:
          statusSettings.disable_functions ?? defaultDisabledFunctions,
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
    upload_max_filesize:
      overrides.upload_max_filesize || base.upload_max_filesize,
    max_file_uploads: overrides.max_file_uploads || base.max_file_uploads,
    default_socket_timeout:
      overrides.default_socket_timeout || base.default_socket_timeout,
    error_reporting: normalizePHPErrorReporting(
      overrides.error_reporting || base.error_reporting,
    ),
    display_errors: overrides.display_errors || base.display_errors,
    disable_functions: overrides.disable_functions || base.disable_functions,
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
    left.display_errors === right.display_errors &&
    left.disable_functions === right.disable_functions
  );
}

function getSelectedPHPVersion(
  status: PHPStatus | null,
  currentVersion?: string | null,
) {
  const normalizedCurrent = currentVersion?.trim();
  if (
    normalizedCurrent &&
    status?.versions?.some(
      (runtimeStatus) => runtimeStatus.version === normalizedCurrent,
    )
  ) {
    return normalizedCurrent;
  }

  const defaultVersion = status?.default_version?.trim();
  if (defaultVersion) {
    return defaultVersion;
  }

  return status?.versions?.[0]?.version ?? "";
}

function getPHPRuntimeStatus(
  status: PHPStatus | null,
  version?: string | null,
): PHPRuntimeStatus | null {
  const selectedVersion = getSelectedPHPVersion(status, version);
  if (!selectedVersion) {
    return null;
  }

  return (
    status?.versions?.find(
      (runtimeStatus) => runtimeStatus.version === selectedVersion,
    ) ?? null
  );
}

function isPHPActionState(state?: string | null) {
  return (
    state === "installing" ||
    state === "removing" ||
    state === "starting" ||
    state === "stopping" ||
    state === "restarting"
  );
}

function isNodeJSProcessRunning(status: DomainNodeJSStatus | null) {
  const processStatus = status?.process?.status?.trim().toLowerCase();
  return (
    processStatus === "online" ||
    processStatus === "launching" ||
    processStatus === "waiting restart"
  );
}

function canStartNodeJSDomain(status: DomainNodeJSStatus | null) {
  if (!status?.supported || !status.configured || !status.pm2_installed) {
    return false;
  }

  return !isNodeJSProcessRunning(status);
}

function canStopNodeJSDomain(status: DomainNodeJSStatus | null) {
  if (!status?.process || status.process.id < 0) {
    return false;
  }

  return isNodeJSProcessRunning(status);
}

function getNodeJSDomainBadge(status: DomainNodeJSStatus | null) {
  if (!status) {
    return "Loading";
  }
  if (!status.supported) {
    return "Unavailable";
  }
  if (!status.configured) {
    return "Not configured";
  }
  if (!status.pm2_installed) {
    return "PM2 required";
  }
  if (!status.process) {
    return "Not started";
  }
  if (isNodeJSProcessRunning(status)) {
    return "Running";
  }

  return status.process.status || "Stopped";
}

function getNodeJSPortFromTarget(target: string) {
  try {
    return new URL(target).port || "80";
  } catch {
    return "";
  }
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

type WordPressSectionTab = "dashboard" | "plugins" | "themes" | "database";
type WordPressDetailsSection = Exclude<WordPressSectionTab, "dashboard">;
type WordPressExtensionListType = "plugin" | "theme";
type WordPressExtensionAction = "activate" | "deactivate" | "delete" | "update";

function getWordPressExtensionLabel(extension: WordPressExtension) {
  return extension.title?.trim() || extension.name;
}

function isWordPressPluginActive(status?: string) {
  return status === "active" || status === "active-network";
}

function canDeleteWordPressPlugin(status?: string) {
  return (
    status !== "active" &&
    status !== "active-network" &&
    status !== "must-use" &&
    status !== "dropin"
  );
}

function canDeleteWordPressTheme(status?: string) {
  return status !== "active";
}

function getWordPressActionLabel(action: WordPressExtensionAction) {
  switch (action) {
    case "activate":
      return { idle: "Enable", busy: "Enabling", done: "enabled" };
    case "deactivate":
      return { idle: "Disable", busy: "Disabling", done: "disabled" };
    case "delete":
      return { idle: "Delete", busy: "Deleting", done: "deleted" };
    case "update":
      return { idle: "Update", busy: "Updating", done: "updated" };
  }
}

function WordPressExtensionList({
  type,
  items,
  busy,
  runningAction,
  onAction,
}: {
  type: WordPressExtensionListType;
  items: WordPressExtension[];
  busy: boolean;
  runningAction: string | null;
  onAction: (name: string, action: WordPressExtensionAction) => void;
}) {
  return (
    <div className="divide-y divide-[var(--app-border)]">
      {items.map((item) => {
        const canEnable =
          type === "plugin"
            ? item.status === "inactive"
            : item.status !== "active";
        const canDisable =
          type === "plugin" && isWordPressPluginActive(item.status);
        const canDelete =
          type === "plugin"
            ? canDeleteWordPressPlugin(item.status)
            : canDeleteWordPressTheme(item.status);
        const updateVersion = item.update_version?.trim();
        const canUpdate =
          item.update === "available" || Boolean(updateVersion);

        return (
          <div
            key={item.name}
            className="grid gap-3 py-3 md:grid-cols-[minmax(0,1fr)_120px_120px_auto]"
          >
            <div className="min-w-0">
              <div
                className="truncate text-sm font-medium text-[var(--app-text)]"
                title={getWordPressExtensionLabel(item)}
              >
                {getWordPressExtensionLabel(item)}
              </div>
              <div
                className="truncate font-mono text-[12px] text-[var(--app-text-muted)]"
                title={item.name}
              >
                {item.name}
              </div>
            </div>
            <div className="text-sm text-[var(--app-text-muted)]">
              {item.status || "Unknown"}
            </div>
            <div className="font-mono text-[12px] text-[var(--app-text-muted)]">
              <div>{item.version || "Unknown"}</div>
              {canUpdate ? (
                <div className="mt-1 text-[11px] text-[var(--app-text-muted)]">
                  Update {updateVersion ? `to ${updateVersion}` : "available"}
                </div>
              ) : null}
            </div>
            <div className="flex flex-wrap gap-2 md:justify-end">
              {canUpdate ? (
                <Button
                  type="button"
                  size="sm"
                  disabled={busy}
                  onClick={() => {
                    onAction(item.name, "update");
                  }}
                >
                  {runningAction === `${type}:update:${item.name}`
                    ? `${getWordPressActionLabel("update").busy}...`
                    : updateVersion
                      ? `Update to ${updateVersion}`
                      : getWordPressActionLabel("update").idle}
                </Button>
              ) : null}
              {canEnable ? (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onClick={() => {
                    onAction(item.name, "activate");
                  }}
                >
                  {runningAction === `${type}:activate:${item.name}`
                    ? `${getWordPressActionLabel("activate").busy}...`
                    : getWordPressActionLabel("activate").idle}
                </Button>
              ) : null}
              {canDisable ? (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onClick={() => {
                    onAction(item.name, "deactivate");
                  }}
                >
                  {runningAction === `${type}:deactivate:${item.name}`
                    ? `${getWordPressActionLabel("deactivate").busy}...`
                    : getWordPressActionLabel("deactivate").idle}
                </Button>
              ) : null}
              {canDelete ? (
                <Button
                  type="button"
                  size="sm"
                  variant="destructive"
                  disabled={busy}
                  onClick={() => {
                    onAction(item.name, "delete");
                  }}
                >
                  {runningAction === `${type}:delete:${item.name}`
                    ? `${getWordPressActionLabel("delete").busy}...`
                    : getWordPressActionLabel("delete").idle}
                </Button>
              ) : null}
            </div>
          </div>
        );
      })}
    </div>
  );
}

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
    title: "Install PHP App",
    icon: FilePlus2,
  },
  {
    title: "Logs",
    icon: File,
  },
  {
    title: "Terminal",
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
    title: "Github",
    icon: GitBranch,
  },
  {
    title: "Website Importing",
    icon: Download,
  },
];
const nodeJSDevToolAction: DomainActionItem = {
  title: "npm install",
  icon: Download,
};
const phpOnlyDevToolTitles = new Set([
  "PHP",
  "Install PHP App",
  "PHP Composer",
]);

const siteBackupTargetKey = "__domain_site_backup__";
const composerManifestName = "composer.json";
const wordPressSectionTabs: Array<{
  value: WordPressSectionTab;
  label: string;
}> = [
  { value: "dashboard", label: "Dashboard" },
  { value: "plugins", label: "Plugins" },
  { value: "themes", label: "Themes" },
  { value: "database", label: "Database" },
];

function createWordPressDetailsLoadedState(
  value = false,
): Record<WordPressDetailsSection, boolean> {
  return {
    plugins: value,
    themes: value,
    database: value,
  };
}

function createWordPressDetailsErrorState(): Record<
  WordPressDetailsSection,
  string | null
> {
  return {
    plugins: null,
    themes: null,
    database: null,
  };
}

function mergeWordPressSectionDetails(
  current: WordPressStatus | null,
  nextStatus: WordPressStatus,
  section: WordPressDetailsSection,
): WordPressStatus {
  if (current === null) {
    return nextStatus;
  }

  switch (section) {
    case "plugins":
      return { ...current, plugins: nextStatus.plugins };
    case "themes":
      return { ...current, themes: nextStatus.themes };
    case "database":
      return { ...current, databases: nextStatus.databases };
  }
}

function isSiteBackedKind(kind: DomainRecord["kind"]) {
  return kind === "Static site" || kind === "Php site";
}

function isRuntimeDomainKind(kind: DomainRecord["kind"] | undefined | null) {
  return kind === "Node.js" || kind === "Python";
}

function getRuntimeDomainLabel(kind: DomainRecord["kind"] | undefined | null) {
  return kind === "Python" ? "Python" : "Node.js";
}

function getComposerManifestPath(path: string | null) {
  if (path === null) {
    return null;
  }

  return path ? `${path}/${composerManifestName}` : composerManifestName;
}

function getActiveDevToolActions(kind: DomainRecord["kind"] | undefined) {
  const items = devToolActions.filter(
    (item) => kind === "Php site" || !phpOnlyDevToolTitles.has(item.title),
  );

  return kind === "Node.js" ? [...items, nodeJSDevToolAction] : items;
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

async function runComposerAction(
  hostname: string,
  action: "install" | "update",
) {
  const response = await fetch(
    `/api/domains/${encodeURIComponent(hostname)}/composer/${action}`,
    {
      method: "POST",
      credentials: "include",
    },
  );

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
      <h2 className="pl-2 text-base font-semibold text-[var(--app-text)]">
        {title}
      </h2>
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
  const [allDomains, setAllDomains] = useState<DomainRecord[]>([]);
  const [sitesBasePath, setSitesBasePath] = useState("");
  const [backups, setBackups] = useState<BackupRecord[]>([]);
  const [databases, setDatabases] = useState<MariaDBDatabase[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [backupDataLoading, setBackupDataLoading] = useState(true);
  const [backupDataError, setBackupDataError] = useState<string | null>(null);
  const [backupDialogOpen, setBackupDialogOpen] = useState(false);
  const [composerDialogOpen, setComposerDialogOpen] = useState(false);
  const [ftpDialogOpen, setFTPDialogOpen] = useState(false);
  const [githubDialogOpen, setGitHubDialogOpen] = useState(false);
  const [phpDialogOpen, setPHPDialogOpen] = useState(false);
  const [templateInstallDialogOpen, setTemplateInstallDialogOpen] =
    useState(false);
  const [terminalDialogOpen, setTerminalDialogOpen] = useState(false);
  const [websiteCopyDialogOpen, setWebsiteCopyDialogOpen] = useState(false);
  const [websiteCopyTargetHostname, setWebsiteCopyTargetHostname] =
    useState("");
  const [websiteCopyReplaceTargetFiles, setWebsiteCopyReplaceTargetFiles] =
    useState(true);
  const [websiteCopyPending, setWebsiteCopyPending] = useState(false);
  const [websiteCopyError, setWebsiteCopyError] = useState<string | null>(null);
  const [websiteCopyFieldErrors, setWebsiteCopyFieldErrors] = useState<
    Record<string, string>
  >({});
  const [composerHasManifest, setComposerHasManifest] = useState(false);
  const [composerPackages, setComposerPackages] = useState<ComposerPackage[]>(
    [],
  );
  const [composerLoading, setComposerLoading] = useState(false);
  const [composerError, setComposerError] = useState<string | null>(null);
  const [composerRunningAction, setComposerRunningAction] = useState<
    "install" | "update" | null
  >(null);
  const [githubForm, setGitHubForm] =
    useState<GitHubFormState>(initialGitHubForm);
  const [savedGitHubForm, setSavedGitHubForm] =
    useState<GitHubFormState>(initialGitHubForm);
  const [githubSaving, setGitHubSaving] = useState(false);
  const [githubDeploying, setGitHubDeploying] = useState(false);
  const [githubError, setGitHubError] = useState<string | null>(null);
  const [githubFeedback, setGitHubFeedback] = useState<string | null>(null);
  const [githubFieldErrors, setGitHubFieldErrors] = useState<
    Record<string, string>
  >({});
  const [creatingBackupTarget, setCreatingBackupTarget] = useState<
    string | null
  >(null);
  const [createdBackupTarget, setCreatedBackupTarget] = useState<string | null>(
    null,
  );
  const [restoringBackupName, setRestoringBackupName] = useState<string | null>(
    null,
  );
  const [restoredBackupName, setRestoredBackupName] = useState<string | null>(
    null,
  );
  const [deletingBackupName, setDeletingBackupName] = useState<string | null>(
    null,
  );
  const [previewUrl, setPreviewUrl] = useState("");
  const [previewLoaded, setPreviewLoaded] = useState(false);
  const [previewError, setPreviewError] = useState(false);
  const [previewErrorMessage, setPreviewErrorMessage] = useState<string | null>(
    null,
  );
  const [previewRefreshing, setPreviewRefreshing] = useState(false);
  const [previewRefreshToken, setPreviewRefreshToken] = useState(0);
  const [nodeJSStatus, setNodeJSStatus] = useState<DomainNodeJSStatus | null>(
    null,
  );
  const [nodeJSLoading, setNodeJSLoading] = useState(false);
  const [nodeJSError, setNodeJSError] = useState<string | null>(null);
  const [nodeJSAction, setNodeJSAction] = useState<
    "start" | "stop" | "install" | null
  >(
    null,
  );
  const [nodeJSLogsOpen, setNodeJSLogsOpen] = useState(false);
  const [nodeJSLogsOutput, setNodeJSLogsOutput] = useState("");
  const [nodeJSLogsLoading, setNodeJSLogsLoading] = useState(false);
  const [nodeJSLogsClearing, setNodeJSLogsClearing] = useState(false);
  const [nodeJSLogsError, setNodeJSLogsError] = useState<string | null>(null);
  const [phpStatus, setPHPStatus] = useState<PHPStatus | null>(null);
  const [phpVersion, setPHPVersion] = useState("");
  const [savedPHPVersion, setSavedPHPVersion] = useState("");
  const [phpForm, setPHPForm] = useState<PHPSettings>(initialPHPSettings);
  const [savedPHPForm, setSavedPHPForm] =
    useState<PHPSettings>(initialPHPSettings);
  const [phpFieldErrors, setPHPFieldErrors] = useState<Record<string, string>>(
    {},
  );
  const [phpLoading, setPHPLoading] = useState(false);
  const [phpSaving, setPHPSaving] = useState(false);
  const [phpError, setPHPError] = useState<string | null>(null);
  const [phpRunningAction, setPHPRunningAction] = useState<
    "install" | "start" | null
  >(null);
  const [wordPressSummary, setWordPressSummary] =
    useState<WordPressSummary | null>(null);
  const [wordPressSectionTab, setWordPressSectionTab] =
    useState<WordPressSectionTab>("dashboard");
  const [wordPressDetails, setWordPressDetails] =
    useState<WordPressStatus | null>(null);
  const [wordPressDetailsLoadedSections, setWordPressDetailsLoadedSections] =
    useState(createWordPressDetailsLoadedState);
  const [wordPressDetailsLoadingSection, setWordPressDetailsLoadingSection] =
    useState<WordPressDetailsSection | null>(null);
  const [wordPressDetailsErrors, setWordPressDetailsErrors] = useState(
    createWordPressDetailsErrorState,
  );
  const [wordPressRunningAction, setWordPressRunningAction] = useState<
    string | null
  >(null);
  const [wordPressInstallDialogType, setWordPressInstallDialogType] = useState<
    WordPressExtensionListType | null
  >(null);
  const createdBackupTimeoutRef = useRef<number | null>(null);
  const restoredBackupTimeoutRef = useRef<number | null>(null);
  const nodeJSLogsRequestRef = useRef(0);
  const wordPressDetailsRequestRef = useRef(0);
  const siteUrl = domain ? getDomainSiteUrl(domain.hostname) : "";
  const wordPressDetailSection =
    wordPressSectionTab === "dashboard" ? null : wordPressSectionTab;
  const currentWordPressDetailsLoaded = wordPressDetailSection
    ? wordPressDetailsLoadedSections[wordPressDetailSection]
    : true;
  const currentWordPressDetailsLoading =
    wordPressDetailSection !== null &&
    wordPressDetailsLoadingSection === wordPressDetailSection;
  const currentWordPressDetailsError = wordPressDetailSection
    ? wordPressDetailsErrors[wordPressDetailSection]
    : null;

  useEffect(() => {
    let active = true;
    setLoading(true);
    setLoadError(null);
    setDomain(null);
    setAllDomains([]);
    setSitesBasePath("");
    setBackups([]);
    setDatabases([]);
    setBackupDataLoading(true);
    setBackupDataError(null);
    setBackupDialogOpen(false);
    setComposerDialogOpen(false);
    setFTPDialogOpen(false);
    setGitHubDialogOpen(false);
    setPHPDialogOpen(false);
    setTemplateInstallDialogOpen(false);
    setTerminalDialogOpen(false);
    setWebsiteCopyDialogOpen(false);
    setWebsiteCopyTargetHostname("");
    setWebsiteCopyReplaceTargetFiles(true);
    setWebsiteCopyPending(false);
    setWebsiteCopyError(null);
    setWebsiteCopyFieldErrors({});
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
    setNodeJSStatus(null);
    setNodeJSLoading(false);
    setNodeJSError(null);
    setNodeJSAction(null);
    setNodeJSLogsOpen(false);
    setNodeJSLogsOutput("");
    setNodeJSLogsLoading(false);
    setNodeJSLogsClearing(false);
    setNodeJSLogsError(null);
    nodeJSLogsRequestRef.current += 1;
    setPHPStatus(null);
    setPHPVersion("");
    setSavedPHPVersion("");
    setPHPForm(initialPHPSettings);
    setSavedPHPForm(initialPHPSettings);
    setPHPFieldErrors({});
    setPHPLoading(false);
    setPHPSaving(false);
    setPHPError(null);
    setPHPRunningAction(null);
    setWordPressSummary(null);
    setWordPressSectionTab("dashboard");
    setWordPressDetails(null);
    setWordPressDetailsLoadedSections(createWordPressDetailsLoadedState());
    setWordPressDetailsLoadingSection(null);
    setWordPressDetailsErrors(createWordPressDetailsErrorState());
    setWordPressRunningAction(null);
    setWordPressInstallDialogType(null);
    wordPressDetailsRequestRef.current += 1;

    async function loadDomain() {
      const [domainsResult, backupsResult, databasesResult] =
        await Promise.allSettled([
          fetchDomains(),
          fetchBackups(),
          fetchMariaDBDatabases(),
        ]);
      if (!active) {
        return;
      }

      if (domainsResult.status === "fulfilled") {
        const matchedDomain =
          domainsResult.value.domains.find(
            (record) => record.hostname === hostname,
          ) ?? null;
        const nextGitHubForm = toGitHubFormState(matchedDomain);

        setSitesBasePath(domainsResult.value.sites_base_path);
        setAllDomains(domainsResult.value.domains);
        setDomain(matchedDomain);
        setGitHubForm(nextGitHubForm);
        setSavedGitHubForm(nextGitHubForm);
        setGitHubError(null);
        setGitHubFieldErrors({});
        setLoadError(
          matchedDomain ? null : "The selected domain could not be found.",
        );
      } else {
        setLoadError(
          getErrorMessage(
            domainsResult.reason,
            "Failed to load domain details.",
          ),
        );
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

      setBackupDataError(
        backupErrors.length > 0 ? backupErrors.join(" ") : null,
      );
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
        setPreviewErrorMessage(
          getErrorMessage(error, "Preview is unavailable right now."),
        );
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
    if (!domain || !isRuntimeDomainKind(domain.kind)) {
      setNodeJSStatus(null);
      setNodeJSLoading(false);
      setNodeJSError(null);
      setNodeJSAction(null);
      setNodeJSLogsOpen(false);
      setNodeJSLogsOutput("");
      setNodeJSLogsLoading(false);
      setNodeJSLogsClearing(false);
      setNodeJSLogsError(null);
      nodeJSLogsRequestRef.current += 1;
      return;
    }

    let active = true;
    setNodeJSLoading(true);
    setNodeJSError(null);

    async function loadNodeJSStatus() {
      try {
        const nextStatus = await fetchDomainNodeJSStatus(domain.hostname);
        if (!active) {
          return;
        }

        setNodeJSStatus(nextStatus);
      } catch (error) {
        if (!active) {
          return;
        }

        setNodeJSStatus(null);
        setNodeJSError(
          getErrorMessage(
            error,
            `Failed to load the ${getRuntimeDomainLabel(domain.kind)} runtime status.`,
          ),
        );
      } finally {
        if (active) {
          setNodeJSLoading(false);
        }
      }
    }

    void loadNodeJSStatus();

    return () => {
      active = false;
    };
  }, [domain?.hostname, domain?.kind]);

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
        const nextVersion = getSelectedPHPVersion(
          nextStatus,
          domain?.php_version,
        );
        const nextRuntime = getPHPRuntimeStatus(nextStatus, nextVersion);
        const nextForm = toPHPSettingsForm(nextRuntime, domain?.php_settings);
        setPHPVersion(nextVersion);
        setSavedPHPVersion(nextVersion);
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

    let intervalId: number | null = null;
    if (isPHPActionState(phpStatus?.state)) {
      intervalId = window.setInterval(() => {
        void loadPHPStatus();
      }, 3_000);
    }

    return () => {
      active = false;
      if (intervalId !== null) {
        window.clearInterval(intervalId);
      }
    };
  }, [
    domain?.hostname,
    domain?.kind,
    domain?.php_settings,
    phpDialogOpen,
    phpStatus?.state,
  ]);

  useEffect(() => {
    if (!domain || domain.kind !== "Php site") {
      setWordPressSummary(null);
      return;
    }

    let active = true;

    async function loadWordPressStatus() {
      try {
        const nextStatus = await fetchDomainWordPressSummary(domain.hostname);
        if (!active) {
          return;
        }

        setWordPressSummary(nextStatus);
      } catch {
        if (active) {
          setWordPressSummary(null);
        }
      }
    }

    void loadWordPressStatus();

    return () => {
      active = false;
    };
  }, [domain?.hostname, domain?.kind]);

  useEffect(() => {
    setWordPressSectionTab("dashboard");
    setWordPressDetails(null);
    setWordPressDetailsLoadedSections(createWordPressDetailsLoadedState());
    setWordPressDetailsLoadingSection(null);
    setWordPressDetailsErrors(createWordPressDetailsErrorState());
    setWordPressRunningAction(null);
    setWordPressInstallDialogType(null);
    wordPressDetailsRequestRef.current += 1;
  }, [domain?.hostname]);

  useEffect(() => {
    if (
      !wordPressDetailSection ||
      !domain ||
      domain.kind !== "Php site" ||
      wordPressDetailsLoadedSections[wordPressDetailSection]
    ) {
      return;
    }

    void loadWordPressDetails(wordPressDetailSection);
  }, [
    domain?.hostname,
    domain?.kind,
    wordPressDetailSection,
    wordPressDetailsLoadedSections,
  ]);

  const filesPath = domain
    ? getFilesPathFromDomainTarget(
        domain.kind,
        domain.hostname,
        sitesBasePath,
        domain.target,
      )
    : null;
  const documentRootDisplayPath = domain
    ? getDocumentRootDisplayPath(
        domain.kind,
        domain.hostname,
        sitesBasePath,
        domain.target,
      )
    : "";
  const terminalPathLabel = filesPath || "/";
  const composerManifestPath = getComposerManifestPath(filesPath);
  const websiteCopyTargets = allDomains.filter(
    (record) => record.hostname !== domain?.hostname,
  );
  const runtimeLabel = getRuntimeDomainLabel(domain?.kind);
  const runtimeVersion =
    domain?.kind === "Python" && !nodeJSLoading && !nodeJSError
      ? nodeJSStatus?.runtime_version ?? ""
      : "";
  const nodeJSPort =
    domain && isRuntimeDomainKind(domain.kind)
      ? getNodeJSPortFromTarget(domain.target)
      : "";
  const nodeJSRunning = isNodeJSProcessRunning(nodeJSStatus);
  const nodeJSStatusLabel = nodeJSLoading
    ? "Loading"
    : nodeJSError
      ? "Unavailable"
      : getNodeJSDomainBadge(nodeJSStatus);
  const nodeJSToggleAction = nodeJSRunning ? "stop" : "start";
  const nodeJSStartDisabled =
    nodeJSLoading || nodeJSAction !== null || !canStartNodeJSDomain(nodeJSStatus);
  const nodeJSStopDisabled =
    nodeJSLoading || nodeJSAction !== null || !canStopNodeJSDomain(nodeJSStatus);
  const nodeJSToggleDisabled = nodeJSRunning
    ? nodeJSStopDisabled
    : nodeJSStartDisabled;
  const nodeJSLogsProcess =
    nodeJSStatus?.process && nodeJSStatus.process.id >= 0
      ? nodeJSStatus.process
      : null;
  const nodeJSLogsDisabled =
    nodeJSLoading || nodeJSAction !== null || nodeJSLogsProcess === null;
  const nodeJSLogsLineCount = nodeJSLogsOutput
    ? nodeJSLogsOutput.split(/\r?\n/).length
    : 0;
  const activeDevToolActions = getActiveDevToolActions(domain?.kind);

  async function loadNodeJSLogs(processID: number, processName: string) {
    const requestID = nodeJSLogsRequestRef.current + 1;
    nodeJSLogsRequestRef.current = requestID;
    setNodeJSLogsLoading(true);
    setNodeJSLogsError(null);

    try {
      const output = await fetchPM2ProcessLogs(processID);
      if (nodeJSLogsRequestRef.current !== requestID) {
        return;
      }

      setNodeJSLogsOutput(output.trim());
    } catch (error) {
      if (nodeJSLogsRequestRef.current !== requestID) {
        return;
      }

      setNodeJSLogsOutput("");
      setNodeJSLogsError(
        getErrorMessage(error, `Failed to load logs for ${processName}.`),
      );
    } finally {
      if (nodeJSLogsRequestRef.current === requestID) {
        setNodeJSLogsLoading(false);
      }
    }
  }

  async function handleClearNodeJSLogs() {
    if (nodeJSLogsProcess === null || nodeJSLogsLoading || nodeJSLogsClearing) {
      return;
    }

    const requestID = nodeJSLogsRequestRef.current + 1;
    nodeJSLogsRequestRef.current = requestID;
    setNodeJSLogsClearing(true);
    setNodeJSLogsError(null);

    try {
      await clearPM2ProcessLogs(nodeJSLogsProcess.id);
      if (nodeJSLogsRequestRef.current !== requestID) {
        return;
      }

      setNodeJSLogsOutput("");
      toast.success("PM2 logs cleared.");
    } catch (error) {
      if (nodeJSLogsRequestRef.current !== requestID) {
        return;
      }

      setNodeJSLogsError(
        getErrorMessage(
          error,
          `Failed to clear logs for ${nodeJSLogsProcess.name}.`,
        ),
      );
      toast.error(
        getErrorMessage(
          error,
          `Failed to clear logs for ${nodeJSLogsProcess.name}.`,
        ),
      );
    } finally {
      if (nodeJSLogsRequestRef.current === requestID) {
        setNodeJSLogsLoading(false);
        setNodeJSLogsClearing(false);
      }
    }
  }

  function handleNodeJSLogsOpenChange(open: boolean) {
    setNodeJSLogsOpen(open);
    if (open) {
      return;
    }

    nodeJSLogsRequestRef.current += 1;
    setNodeJSLogsOutput("");
    setNodeJSLogsLoading(false);
    setNodeJSLogsClearing(false);
    setNodeJSLogsError(null);
  }

  function openNodeJSLogs() {
    if (nodeJSLogsProcess === null) {
      return;
    }

    setNodeJSLogsOpen(true);
    setNodeJSLogsOutput("");
    setNodeJSLogsLoading(true);
    setNodeJSLogsClearing(false);
    setNodeJSLogsError(null);
    void loadNodeJSLogs(nodeJSLogsProcess.id, nodeJSLogsProcess.name);
  }

  useEffect(() => {
    if (!websiteCopyDialogOpen) {
      return;
    }

    if (websiteCopyTargets.length === 0) {
      if (websiteCopyTargetHostname !== "") {
        setWebsiteCopyTargetHostname("");
      }
      return;
    }

    const hasSelectedTarget = websiteCopyTargets.some(
      (record) => record.hostname === websiteCopyTargetHostname,
    );
    if (!hasSelectedTarget) {
      setWebsiteCopyTargetHostname(websiteCopyTargets[0]?.hostname ?? "");
    }
  }, [websiteCopyDialogOpen, websiteCopyTargetHostname, websiteCopyTargets]);

  useEffect(() => {
    if (
      !composerDialogOpen ||
      !domain ||
      domain.kind !== "Php site" ||
      composerManifestPath === null
    ) {
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

        const message = getErrorMessage(
          error,
          "Failed to load Composer details.",
        );
        setComposerHasManifest(false);
        setComposerPackages([]);
        setComposerError(
          message === "file or directory not found" ? null : message,
        );
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

  const showSiteBackups = domain !== null;
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
  const activePHPRuntime = getPHPRuntimeStatus(phpStatus, phpVersion);
  const phpDirty =
    phpVersion !== savedPHPVersion || !samePHPSettings(phpForm, savedPHPForm);

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
        getErrorMessage(
          error,
          `Failed to create backup for ${domain.hostname}.`,
        ),
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
        setCreatedBackupTarget((current) =>
          current === name ? null : current,
        );
        createdBackupTimeoutRef.current = null;
      }, 1500);
      toast.success(`Created local backup ${record.name}.`);
    } catch (error) {
      toast.error(
        getErrorMessage(error, `Failed to create local backup for ${name}.`),
      );
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
      const result = await restoreBackup(name, "local");
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
        result.restored_sites?.some(
          (restoredHostname) => restoredHostname === domain.hostname,
        )
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
      await deleteBackup(name, "local");
      setBackups((current) => current.filter((item) => item.name !== name));
      toast.success(`Deleted backup ${name}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, `Failed to delete ${name}.`));
    } finally {
      setDeletingBackupName(null);
    }
  }

  async function handleComposerAction(action: "install" | "update") {
    if (
      !domain ||
      domain.kind !== "Php site" ||
      composerManifestPath === null ||
      composerRunningAction !== null
    ) {
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
      const message = getErrorMessage(
        error,
        `Failed to run composer ${action}.`,
      );
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
      const updatedDomain = await updateDomainGitHubIntegration(
        domain.hostname,
        {
          repository_url: nextForm.repositoryUrl.trim(),
          auto_deploy_on_push: nextForm.autoDeployOnPush,
          post_fetch_script: nextForm.postFetchScript.trim(),
        },
      );
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
      const message = getErrorMessage(
        error,
        "Failed to save GitHub integration.",
      );
      setGitHubError(message);
      setGitHubFieldErrors(
        typeof error === "object" && error !== null && "fieldErrors" in error
          ? ((error as { fieldErrors?: Record<string, string> }).fieldErrors ??
              {})
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

  async function handleInstallPHP() {
    setPHPRunningAction("install");
    setPHPError(null);

    try {
      const nextStatus = await installPHP(phpVersion || undefined);
      setPHPStatus(nextStatus);
      const nextVersion = getSelectedPHPVersion(nextStatus, phpVersion);
      const nextRuntime = getPHPRuntimeStatus(nextStatus, nextVersion);
      const nextForm = toPHPSettingsForm(nextRuntime, domain?.php_settings);
      setPHPVersion(nextVersion);
      setSavedPHPVersion(nextVersion);
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
      const nextStatus = await startPHP(phpVersion || undefined);
      setPHPStatus(nextStatus);
      const nextVersion = getSelectedPHPVersion(nextStatus, phpVersion);
      const nextRuntime = getPHPRuntimeStatus(nextStatus, nextVersion);
      const nextForm = toPHPSettingsForm(nextRuntime, domain?.php_settings);
      setPHPVersion(nextVersion);
      setSavedPHPVersion(nextVersion);
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
        php_version: phpVersion,
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
        disable_functions: phpForm.disable_functions ?? "",
      });
      const nextRuntime = getPHPRuntimeStatus(
        phpStatus,
        updatedDomain.php_version,
      );
      const nextForm = toPHPSettingsForm(
        nextRuntime,
        updatedDomain.php_settings,
      );
      setDomain(updatedDomain);
      setPHPVersion(updatedDomain.php_version ?? "");
      setSavedPHPVersion(updatedDomain.php_version ?? "");
      setPHPForm(nextForm);
      setSavedPHPForm(nextForm);
      setPHPDialogOpen(false);
      toast.success("PHP settings saved.");
    } catch (error) {
      const phpSettingsError = error as DomainApiError;
      setPHPFieldErrors(phpSettingsError.fieldErrors ?? {});
      const message =
        phpSettingsError.message || "PHP settings could not be saved.";
      setPHPError(message);
      toast.error(message);
    } finally {
      setPHPSaving(false);
    }
  }

  async function handleCopyWebsite() {
    if (!domain || websiteCopyPending) {
      return;
    }

    setWebsiteCopyPending(true);
    setWebsiteCopyError(null);
    setWebsiteCopyFieldErrors({});

    try {
      await copyDomainWebsite(domain.hostname, {
        target_hostname: websiteCopyTargetHostname.trim(),
        replace_target_files: websiteCopyReplaceTargetFiles,
      });
      setWebsiteCopyDialogOpen(false);
      toast.success(
        `Copied website files from ${domain.hostname} to ${websiteCopyTargetHostname}.`,
      );
    } catch (error) {
      const copyError = error as DomainApiError;
      setWebsiteCopyFieldErrors(copyError.fieldErrors ?? {});
      const message = copyError.message || "Website files could not be copied.";
      setWebsiteCopyError(message);
      toast.error(message);
    } finally {
      setWebsiteCopyPending(false);
    }
  }

  async function handleNodeJSAction(action: "start" | "stop") {
    if (!domain || !isRuntimeDomainKind(domain.kind) || nodeJSAction !== null) {
      return;
    }

    setNodeJSAction(action);
    setNodeJSError(null);
    const currentRuntimeLabel = getRuntimeDomainLabel(domain.kind);

    try {
      const nextStatus =
        action === "start"
          ? await startDomainNodeJS(domain.hostname)
          : await stopDomainNodeJS(domain.hostname);
      setNodeJSStatus(nextStatus);
      toast.success(
        action === "start"
          ? `Started ${currentRuntimeLabel} for ${domain.hostname}.`
          : `Stopped ${currentRuntimeLabel} for ${domain.hostname}.`,
      );
    } catch (error) {
      const message = getErrorMessage(
        error,
        action === "start"
          ? `Failed to start the ${currentRuntimeLabel} app.`
          : `Failed to stop the ${currentRuntimeLabel} app.`,
      );
      setNodeJSError(message);
      toast.error(message);
    } finally {
      setNodeJSAction(null);
    }
  }

  async function handleNodeJSInstall() {
    if (!domain || domain.kind !== "Node.js" || nodeJSAction !== null) {
      return;
    }

    setNodeJSAction("install");
    setNodeJSError(null);

    try {
      await installDomainNodeJSPackages(domain.hostname);
      toast.success(`Installed npm packages for ${domain.hostname}.`);
    } catch (error) {
      const message = getErrorMessage(error, "Failed to run npm install.");
      setNodeJSError(message);
      toast.error(message);
    } finally {
      setNodeJSAction(null);
    }
  }

  async function loadWordPressDetails(section: WordPressDetailsSection) {
    if (
      !domain ||
      domain.kind !== "Php site" ||
      wordPressDetailsLoadedSections[section]
    ) {
      return;
    }

    const requestID = wordPressDetailsRequestRef.current + 1;
    wordPressDetailsRequestRef.current = requestID;
    setWordPressDetailsLoadingSection(section);
    setWordPressDetailsErrors((current) => ({
      ...current,
      [section]: null,
    }));

    try {
      const nextStatus = await fetchDomainWordPressStatus(domain.hostname, {
        section,
      });
      if (wordPressDetailsRequestRef.current !== requestID) {
        return;
      }

      setWordPressDetails((current) =>
        mergeWordPressSectionDetails(current, nextStatus, section),
      );
      setWordPressDetailsLoadedSections((current) => ({
        ...current,
        [section]: true,
      }));
    } catch (error) {
      if (wordPressDetailsRequestRef.current !== requestID) {
        return;
      }

      setWordPressDetailsErrors((current) => ({
        ...current,
        [section]: getErrorMessage(
          error,
          `Failed to load WordPress ${section}.`,
        ),
      }));
    } finally {
      if (wordPressDetailsRequestRef.current === requestID) {
        setWordPressDetailsLoadingSection(null);
      }
    }
  }

  function applyWordPressStatus(nextStatus: WordPressStatus) {
    setWordPressSummary({
      cli_available: nextStatus.cli_available,
      cli_path: nextStatus.cli_path,
      installed: nextStatus.installed,
      inspect_error: nextStatus.inspect_error,
      version: nextStatus.version,
    });
    setWordPressDetails(nextStatus);
    setWordPressDetailsLoadedSections(createWordPressDetailsLoadedState(true));
    setWordPressDetailsErrors(createWordPressDetailsErrorState());
    setWordPressDetailsLoadingSection(null);
  }

  function applyWordPressSectionStatus(
    nextStatus: WordPressStatus,
    section: WordPressDetailsSection,
  ) {
    setWordPressDetails((current) =>
      mergeWordPressSectionDetails(current, nextStatus, section),
    );
    setWordPressDetailsLoadedSections((current) => ({
      ...current,
      [section]: true,
    }));
    setWordPressDetailsErrors((current) => ({
      ...current,
      [section]: null,
    }));
    setWordPressDetailsLoadingSection(null);
  }

  async function refreshWordPressSummaryAfterTemplateInstall(
    prefetchedStatus?: WordPressStatus | null,
  ) {
    if (prefetchedStatus) {
      applyWordPressStatus(prefetchedStatus);
      setWordPressSectionTab("dashboard");
    }

    if (!domain || domain.kind !== "Php site") {
      setWordPressSummary(null);
      setWordPressDetails(null);
      setWordPressDetailsLoadedSections(createWordPressDetailsLoadedState());
      setWordPressDetailsLoadingSection(null);
      setWordPressDetailsErrors(createWordPressDetailsErrorState());
      setWordPressRunningAction(null);
      return;
    }

    try {
      const nextSummary = await fetchDomainWordPressSummary(domain.hostname);
      setWordPressSummary(nextSummary);
      if (!nextSummary.installed) {
        setWordPressSectionTab("dashboard");
        setWordPressDetails(null);
        setWordPressDetailsLoadedSections(createWordPressDetailsLoadedState());
        setWordPressDetailsLoadingSection(null);
        setWordPressDetailsErrors(createWordPressDetailsErrorState());
        setWordPressRunningAction(null);
      }
    } catch {
      setWordPressSummary(null);
      setWordPressSectionTab("dashboard");
      setWordPressDetails(null);
      setWordPressDetailsLoadedSections(createWordPressDetailsLoadedState());
      setWordPressDetailsLoadingSection(null);
      setWordPressDetailsErrors(createWordPressDetailsErrorState());
      setWordPressRunningAction(null);
    }
  }

  async function handleTemplateInstalled(result: InstallDomainTemplateResult) {
    setPreviewRefreshing(true);
    setPreviewError(false);
    setPreviewErrorMessage(null);
    setPreviewRefreshToken(Date.now());
    await refreshWordPressSummaryAfterTemplateInstall(result.wordpress ?? null);
  }

  async function handleWordPressExtensionAction(
    type: WordPressExtensionListType,
    name: string,
    action: WordPressExtensionAction,
  ) {
    if (!domain || domain.kind !== "Php site") {
      return;
    }

    setWordPressRunningAction(`${type}:${action}:${name}`);

    try {
      const nextStatus =
        type === "plugin"
          ? await runDomainWordPressPluginAction(domain.hostname, {
              name,
              action,
            })
          : await runDomainWordPressThemeAction(domain.hostname, {
              name,
              action,
            });
      applyWordPressSectionStatus(
        nextStatus,
        type === "plugin" ? "plugins" : "themes",
      );
      toast.success(
        `${type === "plugin" ? "Plugin" : "Theme"} ${name} ${getWordPressActionLabel(action).done}.`,
      );
    } catch (error) {
      toast.error(
        getErrorMessage(
          error,
          `Failed to ${getWordPressActionLabel(action).idle.toLowerCase()} ${type} ${name}.`,
        ),
      );
    } finally {
      setWordPressRunningAction(null);
    }
  }

  function getWordPressInstallDialogItems(type: WordPressExtensionListType) {
    if (type === "plugin") {
      return wordPressDetails?.plugins ?? [];
    }

    return wordPressDetails?.themes ?? [];
  }

  return (
    <>
      <DomainWordPressExtensionInstallDialog
        open={wordPressInstallDialogType !== null && domain !== null}
        onOpenChange={(open) => {
          if (!open) {
            setWordPressInstallDialogType(null);
          }
        }}
        hostname={domain?.hostname ?? hostname}
        type={wordPressInstallDialogType ?? "plugin"}
        installedItems={getWordPressInstallDialogItems(
          wordPressInstallDialogType ?? "plugin",
        )}
        onInstalled={(status) => {
          applyWordPressSectionStatus(
            status,
            (wordPressInstallDialogType ?? "plugin") === "plugin"
              ? "plugins"
              : "themes",
          );
        }}
      />
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
        createSiteBackupDisabled={
          creatingBackupTarget !== null || !showSiteBackups
        }
        createSiteBackupBusy={creatingBackupTarget === siteBackupTargetKey}
        createSiteBackupDone={createdBackupTarget === siteBackupTargetKey}
        onCreateDatabaseBackup={(name) => {
          void handleCreateDatabaseBackup(name);
        }}
        createDatabaseBackupDisabled={creatingBackupTarget !== null}
        creatingDatabaseBackupName={creatingBackupTarget}
        createdDatabaseBackupName={
          createdBackupTarget === siteBackupTargetKey
            ? null
            : createdBackupTarget
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
        open={composerDialogOpen && domain?.kind === "Php site"}
        onOpenChange={setComposerDialogOpen}
        hostname={domain?.hostname ?? hostname}
        projectPath={documentRootDisplayPath}
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
      {domain ? (
        <DomainTemplateInstallDialog
          open={templateInstallDialogOpen}
          onOpenChange={setTemplateInstallDialogOpen}
          hostname={domain.hostname}
          documentRoot={documentRootDisplayPath}
          onInstalled={(result) => handleTemplateInstalled(result)}
        />
      ) : null}
      <DomainFTPDialog
        open={ftpDialogOpen && domain !== null}
        domain={domain}
        onOpenChange={setFTPDialogOpen}
      />
      <DomainGitHubDialog
        open={githubDialogOpen && domain !== null}
        onOpenChange={setGitHubDialogOpen}
        hostname={domain?.hostname ?? hostname}
        repositoryUrl={githubForm.repositoryUrl}
        autoDeployOnPush={githubForm.autoDeployOnPush}
        postFetchScript={githubForm.postFetchScript}
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
        onPostFetchScriptChange={(value) => {
          setGitHubError(null);
          setGitHubFeedback(null);
          setGitHubFieldErrors((current) => {
            const next = { ...current };
            delete next.post_fetch_script;
            return next;
          });
          setGitHubForm((current) => ({
            ...current,
            postFetchScript: value,
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
          status={activePHPRuntime}
          availableVersions={phpStatus?.available_versions ?? []}
          selectedVersion={phpVersion}
          form={phpForm}
          fieldErrors={phpFieldErrors}
          loading={phpLoading}
          saving={phpSaving}
          error={phpError}
          dirty={phpDirty}
          runningAction={phpRunningAction}
          onVersionChange={(value) => {
            setPHPError(null);
            setPHPFieldErrors((current) => {
              const next = { ...current };
              delete next.php_version;
              return next;
            });
            setPHPVersion(value);
          }}
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
      {domain ? (
        <DomainWebsiteCopyDialog
          open={websiteCopyDialogOpen}
          onOpenChange={(open) => {
            setWebsiteCopyDialogOpen(open);
            if (!open) {
              setWebsiteCopyError(null);
              setWebsiteCopyFieldErrors({});
            }
          }}
          sourceDomain={domain}
          targets={websiteCopyTargets}
          targetHostname={websiteCopyTargetHostname}
          replaceTargetFiles={websiteCopyReplaceTargetFiles}
          copying={websiteCopyPending}
          error={websiteCopyError}
          fieldErrors={websiteCopyFieldErrors}
          onTargetHostnameChange={(value) => {
            setWebsiteCopyError(null);
            setWebsiteCopyFieldErrors((current) => {
              const next = { ...current };
              delete next.target_hostname;
              return next;
            });
            setWebsiteCopyTargetHostname(value);
          }}
          onReplaceTargetFilesChange={(checked) => {
            setWebsiteCopyReplaceTargetFiles(checked);
          }}
          onCopy={() => {
            void handleCopyWebsite();
          }}
        />
      ) : null}
      <Dialog open={terminalDialogOpen} onOpenChange={setTerminalDialogOpen}>
        <DialogContent className="max-w-6xl gap-0 overflow-hidden p-0 sm:max-w-6xl">
          <DialogHeader className="sr-only">
            <DialogTitle>Terminal</DialogTitle>
            <DialogDescription>{terminalPathLabel}</DialogDescription>
          </DialogHeader>
          <TerminalWindow
            cwd={filesPath || ""}
            cwdLabel={terminalPathLabel}
            title={domain ? `${domain.hostname} terminal` : "Terminal"}
            className="rounded-none border-0 shadow-none"
            heightClassName="h-[24rem] sm:h-[32rem]"
          />
        </DialogContent>
      </Dialog>
      <Dialog open={nodeJSLogsOpen} onOpenChange={handleNodeJSLogsOpenChange}>
        <DialogContent className="h-[min(80vh,calc(100vh-2rem))] grid-rows-[auto_minmax(0,1fr)] overflow-hidden sm:max-w-5xl">
          <DialogHeader className="gap-3">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0 flex-1">
                <DialogTitle>
                  {nodeJSLogsProcess ? `${nodeJSLogsProcess.name} logs` : "PM2 logs"}
                </DialogTitle>
                <DialogDescription>
                  {nodeJSLogsProcess
                    ? `pm2 logs ${nodeJSLogsProcess.id} --lines 200 --nostream --raw`
                    : `Recent PM2 output for the domain ${runtimeLabel} process.`}
                </DialogDescription>
              </div>

              <div className="flex flex-wrap items-center gap-2">
                {nodeJSLogsOutput ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="shrink-0 border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text)] hover:bg-[var(--app-bg-2)]"
                    onClick={() => {
                      if (!nodeJSLogsOutput) {
                        return;
                      }

                      void navigator.clipboard.writeText(nodeJSLogsOutput).then(
                        () => {
                          toast.success("PM2 logs copied.");
                        },
                        () => {
                          toast.error("Failed to copy PM2 logs.");
                        },
                      );
                    }}
                  >
                    <Copy className="h-4 w-4" />
                    Copy logs
                  </Button>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="shrink-0 border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text)] hover:bg-[var(--app-bg-2)]"
                  onClick={() => {
                    void handleClearNodeJSLogs();
                  }}
                  disabled={
                    nodeJSLogsClearing ||
                    nodeJSLogsLoading ||
                    nodeJSLogsProcess === null
                  }
                >
                  {nodeJSLogsClearing ? (
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                  ) : (
                    <Trash2 className="h-4 w-4" />
                  )}
                  Clear logs
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="shrink-0 border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text)] hover:bg-[var(--app-bg-2)]"
                  onClick={() => {
                    if (nodeJSLogsProcess) {
                      void loadNodeJSLogs(
                        nodeJSLogsProcess.id,
                        nodeJSLogsProcess.name,
                      );
                    }
                  }}
                  disabled={
                    nodeJSLogsLoading ||
                    nodeJSLogsClearing ||
                    nodeJSLogsProcess === null
                  }
                >
                  {nodeJSLogsLoading ? (
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                  ) : (
                    <RefreshCw className="h-4 w-4" />
                  )}
                  Refresh
                </Button>
              </div>
            </div>
          </DialogHeader>

          <div className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
            <div className="border-b border-[var(--app-border)] bg-[var(--app-surface)] px-5 py-4 text-sm text-[var(--app-text-muted)]">
              {nodeJSLogsProcess ? (
                <>
                  <span className="font-medium text-[var(--app-text)]">
                    {nodeJSLogsProcess.name}
                  </span>
                  {" • "}
                  {nodeJSLogsLineCount > 0
                    ? `${nodeJSLogsLineCount} ${nodeJSLogsLineCount === 1 ? "line" : "lines"}`
                    : "No captured output"}
                </>
              ) : (
                `Start the ${runtimeLabel} process to load PM2 logs.`
              )}
            </div>

            {nodeJSLogsError ? (
              <div className="flex h-full items-center justify-center p-5 sm:p-6">
                <div className="max-w-xl rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-[var(--app-danger)]">
                  {nodeJSLogsError}
                </div>
              </div>
            ) : nodeJSLogsLoading ? (
              <div className="flex h-full items-center justify-center gap-2 px-5 text-sm text-[var(--app-text-muted)] sm:px-6">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Loading PM2 logs...
              </div>
            ) : nodeJSLogsOutput ? (
              <ScrollArea className="min-h-0 flex-1 bg-[var(--app-surface)]">
                <pre className="p-5 font-mono text-xs leading-5 whitespace-pre-wrap break-words text-[var(--app-text)] sm:p-6">
                  {nodeJSLogsOutput}
                </pre>
              </ScrollArea>
            ) : (
              <div className="flex h-full items-center justify-center px-5 text-sm text-[var(--app-text-muted)] sm:px-6">
                {nodeJSLogsProcess
                  ? "No log output returned for this process."
                  : `Start the ${runtimeLabel} process to load PM2 logs.`}
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>
      <PageHeader
        title={
          loading ? (
            "Domain details"
          ) : domain ? (
            <span className="flex flex-wrap items-center gap-3">
              <span>{domain.hostname}</span>
              <Badge
                asChild
                variant="outline"
                className="rounded-full align-middle"
              >
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
                                setPreviewErrorMessage(
                                  "Preview image could not be displayed.",
                                );
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
                                    ? (previewErrorMessage ??
                                      "Preview is unavailable right now.")
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
                        {domain
                          ? domain.cache_enabled
                            ? "Enabled"
                            : "Disabled"
                          : "..."}
                      </dd>
                    </div>
                  </dl>
                </section>
              </aside>
              <div className="space-y-4">
                {isRuntimeDomainKind(domain?.kind) ? (
                  <section className="overflow-x-auto rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] p-3 shadow-[var(--app-shadow)]">
                    <div className="flex min-w-max items-center gap-4 text-xs">
                      <h2 className="shrink-0 text-sm font-semibold tracking-tight text-[var(--app-text)]">
                        {domain?.kind === "Python" ? (
                          <span className="inline-flex items-baseline gap-1.5">
                            <span>Python</span>
                            {runtimeVersion ? (
                              <span className="text-sm font-medium text-[var(--app-text-muted)]">
                                {runtimeVersion}
                              </span>
                            ) : null}
                          </span>
                        ) : (
                          `${runtimeLabel} Runtime`
                        )}
                      </h2>
                      <div className="inline-flex items-baseline gap-1.5">
                        <span className="shrink-0 text-[var(--app-text-muted)]">
                          Status
                        </span>
                        <span className="text-[var(--app-text)]">
                          {nodeJSStatusLabel}
                        </span>
                      </div>
                      <div className="inline-flex items-baseline gap-1.5">
                        <span className="shrink-0 text-[var(--app-text-muted)]">
                          Port
                        </span>
                        <span className="font-mono text-[var(--app-text)]">
                          {nodeJSPort || "-"}
                        </span>
                      </div>
                      <div className="inline-flex min-w-0 flex-1 items-baseline gap-1.5">
                        <span className="shrink-0 text-[var(--app-text-muted)]">
                          Script path
                        </span>
                        <span
                          className="truncate font-mono text-[var(--app-text)]"
                          title={nodeJSStatus?.script_path || domain.nodejs_script_path || "-"}
                        >
                          {nodeJSStatus?.script_path ||
                            domain.nodejs_script_path ||
                            "-"}
                        </span>
                      </div>
                      <div className="shrink-0">
                        <div className="flex items-center gap-2">
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            className="h-8 gap-1.5 px-3 text-xs"
                            onClick={openNodeJSLogs}
                            disabled={nodeJSLogsDisabled}
                            title={
                              nodeJSLogsProcess === null
                                ? `Start the ${runtimeLabel} process to view PM2 logs.`
                                : undefined
                            }
                          >
                            <TerminalSquare className="h-4 w-4" />
                            PM2 Logs
                          </Button>
                          <Button
                            type="button"
                            size="sm"
                            className="h-8 gap-1.5 px-3 text-xs"
                            onClick={() => {
                              void handleNodeJSAction(nodeJSToggleAction);
                            }}
                            disabled={nodeJSToggleDisabled}
                          >
                            {nodeJSAction === nodeJSToggleAction ? (
                              <LoaderCircle className="h-4 w-4 animate-spin" />
                            ) : nodeJSRunning ? (
                              <PlayerStop className="h-4 w-4" />
                            ) : (
                              <PlayerPlay className="h-4 w-4" />
                            )}
                            {nodeJSRunning ? "Stop" : "Start"}
                          </Button>
                        </div>
                      </div>
                    </div>
                  </section>
                ) : null}
                <DomainActionSection
                  title="Files & Databases"
                  items={fileAndDatabaseActions}
                  onItemClick={(item) => {
                    if (item.title === "Files" && filesPath !== null) {
                      setPendingFilesPath(filesPath);
                      void navigate({
                        to: "/files",
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
                      return;
                    }

                    if (item.title === "FTP") {
                      setFTPDialogOpen(true);
                      return;
                    }

                    if (item.title === "Website Copying" && domain !== null) {
                      if (websiteCopyTargets.length === 0) {
                        toast.error(
                          "No other domains are available to receive a copy.",
                        );
                        return;
                      }

                      setWebsiteCopyError(null);
                      setWebsiteCopyFieldErrors({});
                      setWebsiteCopyReplaceTargetFiles(true);
                      setWebsiteCopyDialogOpen(true);
                    }
                  }}
                />
                <div className="pt-2">
                  <DomainActionSection
                    title="Dev Tools"
                    items={activeDevToolActions}
                    onItemClick={(item) => {
                      if (item.title === "PHP" && domain !== null) {
                        if (domain.kind !== "Php site") {
                          toast.error(
                            "PHP settings are available only for PHP site domains.",
                          );
                          return;
                        }

                        setPHPDialogOpen(true);
                        return;
                      }

                      if (item.title === "Install PHP App" && domain !== null) {
                        if (domain.kind !== "Php site") {
                          toast.error(
                            "PHP app installation is available only for PHP site domains.",
                          );
                          return;
                        }

                        setTemplateInstallDialogOpen(true);
                        return;
                      }

                      if (item.title === "PHP Composer" && domain !== null) {
                        if (domain.kind !== "Php site") {
                          toast.error(
                            "Composer is available only for PHP site domains.",
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

                      if (item.title === "Terminal" && domain !== null) {
                        if (filesPath === null) {
                          toast.error(
                            "Terminal is unavailable for this domain.",
                          );
                          return;
                        }

                        setTerminalDialogOpen(true);
                        return;
                      }

                      if (item.title === "npm install" && domain !== null) {
                        if (domain.kind !== "Node.js") {
                          toast.error(
                            "npm install is available only for Node.js domains.",
                          );
                          return;
                        }

                        void handleNodeJSInstall();
                        return;
                      }

                      if (item.title === "Github" && domain !== null) {
                        setGitHubDialogOpen(true);
                      }
                    }}
                  />
                </div>
                {wordPressSummary?.installed ? (
                  <section className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-surface)] shadow-[var(--app-shadow)]">
                    <div className="border-b border-[var(--app-border)] bg-[var(--app-surface-muted)]">
                      <div className="flex items-center gap-2 px-4 pt-4 pb-2">
                        <BrandWordpress
                          className="h-4 w-4 text-[var(--app-text-muted)]"
                          stroke={1.8}
                        />
                        <div className="text-sm font-medium text-[var(--app-text)]">
                          WordPress
                        </div>
                      </div>
                      <div role="tablist" aria-label="WordPress sections">
                        <div className="flex min-w-0 overflow-x-auto px-4">
                          {wordPressSectionTabs.map(({ value, label }) => {
                            const active = wordPressSectionTab === value;

                            return (
                              <button
                                key={value}
                                role="tab"
                                type="button"
                                aria-selected={active}
                                tabIndex={active ? 0 : -1}
                                className={cn(
                                  "inline-flex border-b-2 px-1 py-3 text-sm font-medium whitespace-nowrap transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--app-accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--app-surface-muted)]",
                                  active
                                    ? "border-[var(--app-text)] text-[var(--app-text)]"
                                    : "border-transparent text-[var(--app-text-muted)] hover:text-[var(--app-text)]",
                                )}
                                onClick={() => {
                                  setWordPressSectionTab(value);
                                }}
                              >
                                <span>{label}</span>
                              </button>
                            );
                          })}
                        </div>
                      </div>
                    </div>

                    <div className="p-4">
                      {wordPressSectionTab === "dashboard" ? (
                        <div className="space-y-1">
                          <div className="text-sm font-medium text-[var(--app-text)]">
                            WordPress {wordPressSummary.version || "Unknown"}
                          </div>
                          <p className="text-sm text-[var(--app-text-muted)]">
                            Open Plugins, Themes, or Database to load more
                            details.
                          </p>
                        </div>
                      ) : currentWordPressDetailsLoading ? (
                        <div className="flex items-center gap-2 text-sm text-[var(--app-text-muted)]">
                          <LoaderCircle className="h-4 w-4 animate-spin" />
                          Loading WordPress {wordPressSectionTab}...
                        </div>
                      ) : !currentWordPressDetailsLoaded &&
                        currentWordPressDetailsError === null ? (
                        <div className="flex items-center gap-2 text-sm text-[var(--app-text-muted)]">
                          <LoaderCircle className="h-4 w-4 animate-spin" />
                          Loading WordPress {wordPressSectionTab}...
                        </div>
                      ) : currentWordPressDetailsError ? (
                        <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
                          {currentWordPressDetailsError}
                        </div>
                      ) : wordPressDetails &&
                        wordPressSectionTab === "plugins" ? (
                        <div className="space-y-4">
                          <div className="flex flex-wrap items-center justify-between gap-3">
                            <div>
                              <div className="text-sm font-medium text-[var(--app-text)]">
                                Plugins
                              </div>
                              <div className="text-sm text-[var(--app-text-muted)]">
                                {wordPressDetails.plugins.length} installed
                              </div>
                            </div>
                            <Button
                              type="button"
                              size="sm"
                              variant="outline"
                              disabled={wordPressRunningAction !== null}
                              onClick={() => {
                                setWordPressInstallDialogType("plugin");
                              }}
                            >
                              Add New Plugin
                            </Button>
                          </div>
                          {wordPressDetails.plugins.length > 0 ? (
                            <WordPressExtensionList
                              type="plugin"
                              items={wordPressDetails.plugins}
                              busy={wordPressRunningAction !== null}
                              runningAction={wordPressRunningAction}
                              onAction={(name, action) => {
                                void handleWordPressExtensionAction(
                                  "plugin",
                                  name,
                                  action,
                                );
                              }}
                            />
                          ) : (
                            <p className="text-sm text-[var(--app-text-muted)]">
                              No plugins were detected for this site.
                            </p>
                          )}
                        </div>
                      ) : wordPressDetails &&
                        wordPressSectionTab === "themes" ? (
                        <div className="space-y-4">
                          <div className="flex flex-wrap items-center justify-between gap-3">
                            <div>
                              <div className="text-sm font-medium text-[var(--app-text)]">
                                Themes
                              </div>
                              <div className="text-sm text-[var(--app-text-muted)]">
                                {wordPressDetails.themes.length} installed
                              </div>
                            </div>
                            <Button
                              type="button"
                              size="sm"
                              variant="outline"
                              disabled={wordPressRunningAction !== null}
                              onClick={() => {
                                setWordPressInstallDialogType("theme");
                              }}
                            >
                              Add New Theme
                            </Button>
                          </div>
                          {wordPressDetails.themes.length > 0 ? (
                            <WordPressExtensionList
                              type="theme"
                              items={wordPressDetails.themes}
                              busy={wordPressRunningAction !== null}
                              runningAction={wordPressRunningAction}
                              onAction={(name, action) => {
                                void handleWordPressExtensionAction(
                                  "theme",
                                  name,
                                  action,
                                );
                              }}
                            />
                          ) : (
                            <p className="text-sm text-[var(--app-text-muted)]">
                              No themes were detected for this site.
                            </p>
                          )}
                        </div>
                      ) : wordPressDetails &&
                        wordPressSectionTab === "database" ? (
                        wordPressDetails.databases.length > 0 ? (
                          <div className="divide-y divide-[var(--app-border)]">
                            {wordPressDetails.databases.map((database) => (
                              <div
                                key={database.name}
                                className="grid gap-2 py-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_140px]"
                              >
                                <div className="min-w-0">
                                  <div className="text-sm font-medium text-[var(--app-text)]">
                                    {database.name}
                                  </div>
                                  <div className="text-[12px] text-[var(--app-text-muted)]">
                                    Database
                                  </div>
                                </div>
                                <div className="min-w-0">
                                  <div className="truncate text-sm text-[var(--app-text-muted)]">
                                    {database.username}
                                  </div>
                                  <div className="text-[12px] text-[var(--app-text-muted)]">
                                    User
                                  </div>
                                </div>
                                <div className="text-sm text-[var(--app-text-muted)]">
                                  {database.host || "localhost"}
                                </div>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <p className="text-sm text-[var(--app-text-muted)]">
                            No linked databases were detected for this site.
                          </p>
                        )
                      ) : (
                        <p className="text-sm text-[var(--app-text-muted)]">
                          Open a tab to load WordPress details.
                        </p>
                      )}
                    </div>
                  </section>
                ) : null}
              </div>
            </section>
          ) : null}
        </div>
      </div>
    </>
  );
}
