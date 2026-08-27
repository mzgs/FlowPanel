import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import {
  Database,
  Download,
  ExternalLink,
  FolderOpen,
  LoaderCircle,
  Package,
  Pencil,
  Plus,
  Trash2,
  UserCog,
  World,
} from "@/components/icons/lucide-icons";
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
import {
  fetchMariaDBDatabases,
  type MariaDBDatabase,
} from "@/api/mariadb";
import { getPHPMyAdminURL } from "@/api/phpmyadmin";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { BackupRecordsDialog } from "@/components/backup-records-dialog";
import { DomainFTPDialog } from "@/components/domain-ftp-dialog";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button, toolbarPrimaryButtonClassName } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  tableActionButtonClassName,
  tableActionGroupClassName,
  tableDangerActionButtonClassName,
} from "@/components/ui/table";
import { getSiteHostnameFromBackupRecord } from "@/lib/backup-records";
import { getFilesPathFromDomainTarget } from "@/lib/domain-targets";
import { setPendingFilesPath } from "@/lib/files-navigation";
import { cn, getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

type FormState = {
  hostname: string;
  kind: DomainKind;
  target: string;
  nodeJSScriptPath: string;
  appBuildCommand: string;
  appBinaryPath: string;
  cacheEnabled: boolean;
};

type FormErrors = {
  hostname?: string;
  kind?: string;
  target?: string;
  nodejs_script_path?: string;
  app_build_command?: string;
  app_binary_path?: string;
};

type FormMode = "create" | "edit";

const domainKinds: DomainKind[] = [
  "Static site",
  "Php site",
  "Node.js",
  "Python",
  "App",
  "Reverse proxy",
];

const defaultAppBuildCommand = "go build -trimpath -o go-app .";
const defaultAppBinaryPath = "go-app";

const initialFormState: FormState = {
  hostname: "",
  kind: "Php site",
  target: "",
  nodeJSScriptPath: "",
  appBuildCommand: defaultAppBuildCommand,
  appBinaryPath: defaultAppBinaryPath,
  cacheEnabled: false,
};

function getDefaultTarget(kind: DomainKind) {
  if (kind === "Node.js" || kind === "Python" || kind === "App") {
    return kind === "Python" ? "8000" : kind === "App" ? "8080" : "3000";
  }

  return kind === "Reverse proxy" ? "http://127.0.0.1:8080" : "";
}

function getDefaultScriptPath(kind: DomainKind) {
  if (kind === "Python") {
    return "app.py";
  }

  return kind === "Node.js" ? "bin/www" : "";
}

const initialDeleteDomainOptions = {
  deleteDatabase: false,
  deleteDocumentRoot: false,
};


const hostnamePattern =
  /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])$/i;

const domainAvatarPalettes = [
  { backgroundColor: "#b45309", color: "#fff7ed" },
  { backgroundColor: "#a16207", color: "#fefce8" },
  { backgroundColor: "#2f6f44", color: "#f0fdf4" },
  { backgroundColor: "#0f766e", color: "#f0fdfa" },
  { backgroundColor: "#1d4ed8", color: "#eff6ff" },
  { backgroundColor: "#4f46e5", color: "#eef2ff" },
  { backgroundColor: "#7c3aed", color: "#f5f3ff" },
  { backgroundColor: "#be185d", color: "#fdf2f8" },
  { backgroundColor: "#5b21b6", color: "#f5f3ff" },
] as const;

const kindConfig: Record<
  DomainKind,
  {
    icon?: typeof World;
    imageSrc?: string;
    targetLabel?: string;
    targetPlaceholder?: string;
    helpText: string;
  }
> = {
  "Static site": {
    icon: World,
    helpText: "FlowPanel uses the default site directory automatically.",
  },
  "Php site": {
    imageSrc: "/application-icons/php.svg",
    helpText:
      "FlowPanel uses the default PHP site directory automatically and requires PHP-FPM to be ready in Overview.",
  },
  "Node.js": {
    imageSrc: "/application-icons/nodejs.svg",
    targetLabel: "Port",
    targetPlaceholder: "3000",
    helpText:
      "FlowPanel proxies this domain to `127.0.0.1` on the port you set here.",
  },
  Python: {
    imageSrc: "/application-icons/python.png",
    targetLabel: "Port",
    targetPlaceholder: "8000",
    helpText:
      "FlowPanel proxies this domain to `127.0.0.1` on the port you set here.",
  },
  App: {
    icon: Package,
    targetLabel: "Port",
    targetPlaceholder: "8080",
    helpText:
      "FlowPanel sets PORT, runs your build command after each GitHub update, then starts the executable with PM2.",
  },
  "Reverse proxy": {
    imageSrc: "/application-icons/proxy_server.png",
    targetLabel: "Upstream URL",
    targetPlaceholder: "http://127.0.0.1:8080",
    helpText: "Requests will be proxied to this upstream service.",
  },
};

function normalizeHostname(value: string) {
  return value.trim().toLowerCase().replace(/\.$/, "");
}

function getDomainInitial(hostname: string) {
  return hostname.trim().match(/[a-z0-9]/i)?.[0]?.toUpperCase() ?? "?";
}

function getDomainAvatarPalette(hostname: string) {
  const hash = Array.from(hostname).reduce(
    (value, char) => value + char.charCodeAt(0),
    0,
  );
  return domainAvatarPalettes[hash % domainAvatarPalettes.length];
}

function getDomainPort(kind: DomainKind, target: string) {
  if (
    kind !== "Node.js" &&
    kind !== "Python" &&
    kind !== "App" &&
    kind !== "Reverse proxy"
  ) {
    return null;
  }

  const trimmed = target.trim();
  if (/^\d+$/.test(trimmed)) {
    return trimmed;
  }

  if (!trimmed.includes("://")) {
    return trimmed.match(/:(\d+)$/)?.[1] ?? null;
  }

  try {
    const url = new URL(trimmed);
    return url.port || (url.protocol === "https:" ? "443" : "80");
  } catch {
    return trimmed.match(/:(\d+)$/)?.[1] ?? null;
  }
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

  if (kind === "Node.js" || kind === "Python" || kind === "App") {
    if (!trimmed) {
      return "Port is required.";
    }

    if (!/^\d+$/.test(trimmed)) {
      return "Enter a port between 1 and 65535.";
    }

    const port = Number.parseInt(trimmed, 10);
    if (port < 1 || port > 65535) {
      return "Enter a port between 1 and 65535.";
    }

    return undefined;
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

function isRuntimeDomainKind(kind: DomainKind) {
  return kind === "Node.js" || kind === "Python";
}

function isManagedAppKind(kind: DomainKind) {
  return isRuntimeDomainKind(kind) || kind === "App";
}

function validateNodeJSScriptPath(value: string) {
  const trimmed = value.trim();
  if (!trimmed) {
    return "Script path is required.";
  }

  const normalized = trimmed.replace(/\\/g, "/");
  if (normalized.startsWith("/")) {
    return "Enter a path relative to the domain root.";
  }
  if (normalized === "." || normalized === ".." || normalized.startsWith("../")) {
    return "Enter a path relative to the domain root.";
  }

  return undefined;
}

function validateAppBuildCommand(value: string) {
  return value.trim() ? undefined : "Build command is required.";
}

function validateAppBinaryPath(value: string) {
  const normalized = value.trim().replace(/\\/g, "/");
  if (!normalized) {
    return "Executable path is required.";
  }
  if (
    normalized.startsWith("/") ||
    normalized === "." ||
    normalized === ".." ||
    normalized.startsWith("../")
  ) {
    return "Enter an executable path inside the domain root.";
  }
  return undefined;
}

function getNodeJSPort(value: string) {
  const trimmed = value.trim();
  if (!trimmed) {
    return "";
  }

  if (/^\d+$/.test(trimmed)) {
    return trimmed;
  }

  try {
    const parsed = new URL(trimmed);
    return parsed.port;
  } catch {
    return "";
  }
}

function getFormTargetValue(kind: DomainKind, target: string) {
  return isManagedAppKind(kind) ? getNodeJSPort(target) : target;
}

function getFormScriptPathValue(kind: DomainKind, scriptPath?: string) {
  return isRuntimeDomainKind(kind)
    ? scriptPath?.trim() || getDefaultScriptPath(kind)
    : "";
}

export function DomainsPage() {
  const navigate = useNavigate();
  const [domains, setDomains] = useState<DomainRecord[]>([]);
  const [databases, setDatabases] = useState<MariaDBDatabase[]>([]);
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
  const [deleteDomainCandidate, setDeleteDomainCandidate] =
    useState<DomainRecord | null>(null);
  const [deleteDomainOptions, setDeleteDomainOptions] = useState(
    initialDeleteDomainOptions,
  );
  const [creatingBackupDomainId, setCreatingBackupDomainId] = useState<
    string | null
  >(null);
  const [restoringBackupName, setRestoringBackupName] = useState<string | null>(
    null,
  );
  const [restoredBackupName, setRestoredBackupName] = useState<string | null>(
    null,
  );
  const [deletingBackupName, setDeletingBackupName] = useState<string | null>(
    null,
  );
  const [createdBackupDomainId, setCreatedBackupDomainId] = useState<
    string | null
  >(null);
  const [downloadingDomainId, setDownloadingDomainId] = useState<string | null>(
    null,
  );
  const [loadError, setLoadError] = useState<string | null>(null);
  const [backupsLoadError, setBackupsLoadError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [backupDialogDomain, setBackupDialogDomain] =
    useState<DomainRecord | null>(null);
  const [ftpDialogDomain, setFTPDialogDomain] = useState<DomainRecord | null>(
    null,
  );
  const hostnameInputRef = useRef<HTMLInputElement | null>(null);
  const createdBackupTimeoutRef = useRef<number | null>(null);
  const restoredBackupTimeoutRef = useRef<number | null>(null);

  useEffect(() => {
    let active = true;

    async function loadData() {
      try {
        const [domainsResult, databasesResult, backupsResult] =
          await Promise.allSettled([
            fetchDomains(),
            fetchMariaDBDatabases(),
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
          setLoadError(
            getErrorMessage(domainsResult.reason, "Failed to load domains."),
          );
        }

        setDatabases(
          databasesResult.status === "fulfilled"
            ? databasesResult.value.databases
            : [],
        );

        if (backupsResult.status === "fulfilled") {
          setBackups(backupsResult.value.backups);
          setBackupsLoadError(null);
        } else {
          setBackups([]);
          setBackupsLoadError(
            getErrorMessage(backupsResult.reason, "Failed to load backups."),
          );
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
  const siteBackups = backups.reduce<Record<string, BackupRecord[]>>(
    (groups, backup) => {
      const hostname = getSiteHostnameFromBackupRecord(backup);
      if (!hostname) {
        return groups;
      }

      if (!groups[hostname]) {
        groups[hostname] = [];
      }
      groups[hostname].push(backup);
      return groups;
    },
    {},
  );
  const selectedDomainBackups = backupDialogDomain
    ? (siteBackups[backupDialogDomain.hostname] ?? [])
    : [];
  const backupDialogCreating =
    backupDialogDomain !== null &&
    creatingBackupDomainId === backupDialogDomain.id;
  const backupDialogCreated =
    backupDialogDomain !== null &&
    createdBackupDomainId === backupDialogDomain.id;
  const deleteDocumentRootAvailable =
    deleteDomainCandidate !== null &&
    getFilesPathFromDomainTarget(
      deleteDomainCandidate.kind,
      deleteDomainCandidate.hostname,
      sitesBasePath,
      deleteDomainCandidate.target,
    ) !== null;
  const deleteDatabaseCheckboxId = "delete-domain-database";
  const deleteDocumentRootCheckboxId = "delete-domain-document-root";

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
      target: isSiteBackedKind(domain.kind)
        ? ""
        : getFormTargetValue(domain.kind, domain.target),
      nodeJSScriptPath: getFormScriptPathValue(
        domain.kind,
        domain.nodejs_script_path,
      ),
      appBuildCommand:
        domain.app_build_command?.trim() || defaultAppBuildCommand,
      appBinaryPath: domain.app_binary_path?.trim() || defaultAppBinaryPath,
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

  function applyAppBuildExample(appBuildCommand: string, appBinaryPath: string) {
    setForm((current) => ({
      ...current,
      appBuildCommand,
      appBinaryPath,
    }));
    setErrors((current) => ({
      ...current,
      app_build_command: undefined,
      app_binary_path: undefined,
    }));
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
    const nodeJSScriptPath = form.nodeJSScriptPath.trim();
    const appBuildCommand = form.appBuildCommand.trim();
    const appBinaryPath = form.appBinaryPath.trim();
    const nextErrors: FormErrors = {
      hostname: validateHostname(hostname),
      target: isSiteBackedKind(form.kind)
        ? undefined
        : validateTarget(form.kind, target),
      nodejs_script_path:
        isRuntimeDomainKind(form.kind)
          ? validateNodeJSScriptPath(nodeJSScriptPath)
          : undefined,
      app_build_command:
        form.kind === "App"
          ? validateAppBuildCommand(appBuildCommand)
          : undefined,
      app_binary_path:
        form.kind === "App" ? validateAppBinaryPath(appBinaryPath) : undefined,
    };

    if (
      !nextErrors.hostname &&
      getDuplicateHostnameError(hostname, domains, editingDomainId)
    ) {
      nextErrors.hostname = "This domain already exists.";
    }

    setErrors(nextErrors);
    if (
      nextErrors.hostname ||
      nextErrors.target ||
      nextErrors.nodejs_script_path ||
      nextErrors.app_build_command ||
      nextErrors.app_binary_path
    ) {
      return;
    }

    setSubmitting(true);
    setFormError(null);

    try {
      const input = {
        hostname,
        kind: form.kind,
        target: isSiteBackedKind(form.kind) ? "" : target,
        nodejs_script_path:
          isRuntimeDomainKind(form.kind) ? nodeJSScriptPath : undefined,
        app_build_command: form.kind === "App" ? appBuildCommand : undefined,
        app_binary_path: form.kind === "App" ? appBinaryPath : undefined,
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
          nodejs_script_path: domainError.fieldErrors.nodejs_script_path,
          app_build_command: domainError.fieldErrors.app_build_command,
          app_binary_path: domainError.fieldErrors.app_binary_path,
        });
      }

      setFormError(
        hasFieldErrors
          ? null
          : getErrorMessage(
              error,
              isEditing
                ? "Failed to update domain."
                : "Failed to create domain.",
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

    setDeleteDomainOptions(initialDeleteDomainOptions);
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
      const result = await deleteDomain(domain.id, deleteDomainOptions);
      setDomains((current) =>
        current.filter((currentDomain) => currentDomain.id !== domain.id),
      );
      setBackupDialogDomain((current) =>
        current?.id === domain.id ? null : current,
      );
      if (editingDomainId === domain.id) {
        setResetOnClose(true);
        setFormOpen(false);
      }
      if (result.warnings.length > 0) {
        toast.message(`Deleted ${domain.hostname} with warnings.`, {
          description: result.warnings.join(" "),
        });
      }
    } catch (error) {
      setLoadError(
        getErrorMessage(error, `Failed to delete ${domain.hostname}.`),
      );
    } finally {
      setDeletingDomainId(null);
      setDeleteDomainOptions(initialDeleteDomainOptions);
      setDeleteDomainCandidate((current) =>
        current?.id === domain.id ? null : current,
      );
    }
  }

  async function handleCreateBackup(domain: DomainRecord) {
    if (creatingBackupDomainId !== null) {
      return;
    }

    setCreatingBackupDomainId(domain.id);
    setCreatedBackupDomainId(null);

    try {
      const record = await createBackup({
        include_panel_data: false,
        include_docker_data: false,
        include_sites: true,
        include_databases: false,
        site_hostnames: [domain.hostname],
      });
      setBackups((current) => [
        record,
        ...current.filter((item) => item.name !== record.name),
      ]);
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
        getErrorMessage(
          error,
          `Failed to create backup for ${domain.hostname}.`,
        ),
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
      toast.error(
        getErrorMessage(error, `Failed to download ${domain.hostname}.`),
      );
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
      const result = await restoreBackup(name, "local");
      for (const warning of result.warnings ?? []) {
        toast.warning(warning);
      }
      if ((result.warnings?.length ?? 0) > 0) {
        return;
      }
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
      await deleteBackup(name, "local");
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
        title={
          backupDialogDomain
            ? `${backupDialogDomain.hostname} backups`
            : "Domain backups"
        }
        backups={selectedDomainBackups}
        onCreateBackup={() => {
          if (backupDialogDomain) {
            void handleCreateBackup(backupDialogDomain);
          }
        }}
        createDisabled={
          backupDialogDomain === null || creatingBackupDomainId !== null
        }
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
      />
      <ActionConfirmDialog
        open={deleteDomainCandidate !== null}
        onOpenChange={(open) => {
          if (!open && deletingDomainId === null) {
            setDeleteDomainOptions(initialDeleteDomainOptions);
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
        isLoading={
          deleteDomainCandidate !== null &&
          deletingDomainId === deleteDomainCandidate.id
        }
        handleConfirm={() => {
          void confirmDeleteDomain();
        }}
        className="sm:max-w-lg"
      >
        <div className="space-y-3 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] p-4">
          <div className="flex items-start gap-3">
            <Checkbox
              id={deleteDatabaseCheckboxId}
              checked={deleteDomainOptions.deleteDatabase}
              disabled={deletingDomainId !== null}
              onCheckedChange={(checked) =>
                setDeleteDomainOptions((current) => ({
                  ...current,
                  deleteDatabase: checked === true,
                }))
              }
            />
            <div className="space-y-1">
              <Label
                htmlFor={deleteDatabaseCheckboxId}
                className="cursor-pointer text-sm font-medium text-[var(--app-text)]"
              >
                Delete database
              </Label>
              <p className="text-sm text-[var(--app-text-muted)]">
                Also remove MariaDB databases linked to this domain.
              </p>
            </div>
          </div>

          <div className="flex items-start gap-3">
            <Checkbox
              id={deleteDocumentRootCheckboxId}
              checked={deleteDomainOptions.deleteDocumentRoot}
              disabled={
                deletingDomainId !== null || !deleteDocumentRootAvailable
              }
              onCheckedChange={(checked) =>
                setDeleteDomainOptions((current) => ({
                  ...current,
                  deleteDocumentRoot: checked === true,
                }))
              }
            />
            <div className="space-y-1">
              <Label
                htmlFor={deleteDocumentRootCheckboxId}
                className={cn(
                  "text-sm font-medium",
                  deleteDocumentRootAvailable
                    ? "cursor-pointer text-[var(--app-text)]"
                    : "cursor-not-allowed text-[var(--app-text-muted)]",
                )}
              >
                Delete document root
              </Label>
              <p className="text-sm text-[var(--app-text-muted)]">
                {deleteDocumentRootAvailable
                  ? "Also remove the site directory for this domain."
                  : "Available for static and PHP domains with a local site directory."}
              </p>
            </div>
          </div>
        </div>
      </ActionConfirmDialog>

      <DomainFTPDialog
        open={ftpDialogDomain !== null}
        domain={ftpDialogDomain}
        onOpenChange={(open) => {
          if (!open) {
            setFTPDialogDomain(null);
          }
        }}
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
              className={toolbarPrimaryButtonClassName}
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

            <section className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
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
                        <TableHead className="w-[260px] text-right">
                          Actions
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {domains.map((domain) => {
                        const filesPath = getFilesPathFromDomainTarget(
                          domain.kind,
                          domain.hostname,
                          sitesBasePath,
                          domain.target,
                        );
                        const backupCount =
                          siteBackups[domain.hostname]?.length ?? 0;
                        const port = getDomainPort(domain.kind, domain.target);
                        const firstDatabase = databases.find(
                          (database) => database.domain === domain.hostname,
                        );

                        return (
                          <TableRow
                            key={domain.id}
                            className="cursor-pointer"
                            onClick={(event) => {
                              if (
                                (event.target as HTMLElement).closest(
                                  "a, button, input, select, textarea, [role=button]",
                                )
                              ) {
                                return;
                              }

                              void navigate({
                                to: "/domains/$hostname",
                                params: { hostname: domain.hostname },
                              });
                            }}
                          >
                            <TableCell className="font-medium text-[var(--app-text)]">
                              <div className="flex items-center gap-2.5">
                                <span
                                  aria-hidden="true"
                                  className="flex size-7 shrink-0 items-center justify-center rounded-full text-[15px] font-semibold"
                                  style={getDomainAvatarPalette(domain.hostname)}
                                >
                                  {getDomainInitial(domain.hostname)}
                                </span>
                                <div className="min-w-0 flex flex-wrap items-center gap-2">
                                  <span>{domain.hostname}</span>
                                  <Badge
                                    asChild
                                    variant="outline"
                                    className="rounded-full"
                                  >
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
                              </div>
                            </TableCell>
                            <TableCell>
                              {domain.kind}
                              {port ? (
                                <span className="ml-1 text-[12px] text-[var(--app-text-muted)]">
                                  :{port}
                                </span>
                              ) : null}
                            </TableCell>
                            <TableCell>
                              {backupsLoading ? (
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
                            <TableCell className="w-[260px]">
                              <div className={tableActionGroupClassName}>
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
                                    className={tableActionButtonClassName}
                                  >
                                    {downloadingDomainId === domain.id ? (
                                      <LoaderCircle
                                        className="size-4 animate-spin"
                                      />
                                    ) : (
                                      <Download
                                        className="size-4"
                                      />
                                    )}
                                  </Button>
                                ) : null}
                                {filesPath !== null ? (
                                  <Button
                                    variant="ghost"
                                    size="icon"
                                    type="button"
                                    onClick={() => {
                                      setPendingFilesPath(filesPath);
                                      void navigate({ to: "/files" });
                                    }}
                                    aria-label={`Open site folder for ${domain.hostname}`}
                                    title="Open site folder"
                                    className={tableActionButtonClassName}
                                  >
                                    <FolderOpen
                                      className="size-4"
                                    />
                                  </Button>
                                ) : null}
                                <Button
                                  asChild
                                  variant="ghost"
                                  size="icon"
                                  className={tableActionButtonClassName}
                                >
                                  <a
                                    href={getPHPMyAdminURL(
                                      domain.hostname,
                                      firstDatabase?.name,
                                    )}
                                    target="_blank"
                                    rel="noreferrer"
                                    aria-label={`Open phpMyAdmin for ${domain.hostname}`}
                                    title="Open phpMyAdmin"
                                  >
                                    <Database className="size-4" />
                                  </a>
                                </Button>
                                <Button
                                  type="button"
                                  variant="ghost"
                                  size="icon"
                                  onClick={() => {
                                    setFTPDialogDomain(domain);
                                  }}
                                  disabled={deletingDomainId !== null}
                                  aria-label={`Manage FTP for ${domain.hostname}`}
                                  title="Manage FTP"
                                  className={tableActionButtonClassName}
                                >
                                  <UserCog
                                    className="size-4"
                                  />
                                </Button>
                                <Button
                                  type="button"
                                  variant="ghost"
                                  size="icon"
                                  onClick={() => openEditForm(domain)}
                                  disabled={deletingDomainId !== null}
                                  aria-label={`Edit ${domain.hostname}`}
                                  title="Edit"
                                  className={tableActionButtonClassName}
                                >
                                  <Pencil
                                    className="size-4"
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
                                  className={tableDangerActionButtonClassName}
                                  aria-label={`Delete ${domain.hostname}`}
                                  title="Delete"
                                >
                                  {deletingDomainId === domain.id ? (
                                    <LoaderCircle
                                      className="size-4 animate-spin"
                                    />
                                  ) : (
                                    <Trash2
                                      className="size-4"
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
                      Click{" "}
                      <span className="font-medium text-[var(--app-text)]">
                        Add domain
                      </span>{" "}
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
            if (
              event.target !== event.currentTarget ||
              formOpen ||
              !resetOnClose
            ) {
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
            <DialogTitle>
              {isEditing ? "Edit domain" : "New domain"}
            </DialogTitle>
            <DialogDescription>
              {isEditing
                ? "Update the route target and domain type. Domains stay fixed after creation."
                : "Define the domain and route target."}
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
                <p className="text-[12px] text-[var(--app-danger)]">
                  {errors.hostname}
                </p>
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
                className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6"
              >
                {domainKinds.map((kind) => {
                  const isActive = form.kind === kind;
                  const { icon: KindIcon, imageSrc } = kindConfig[kind];

                  return (
                    <button
                      key={kind}
                      type="button"
                      onClick={() => {
                        setForm((current) => ({
                          ...current,
                          kind,
                          target:
                            current.kind === kind
                              ? current.target
                              : getDefaultTarget(kind),
                          nodeJSScriptPath:
                            current.kind === kind
                              ? current.nodeJSScriptPath
                              : getDefaultScriptPath(kind),
                          appBuildCommand:
                            current.kind === kind
                              ? current.appBuildCommand
                              : defaultAppBuildCommand,
                          appBinaryPath:
                            current.kind === kind
                              ? current.appBinaryPath
                              : defaultAppBinaryPath,
                        }));
                        setErrors((current) => ({
                          ...current,
                          kind: undefined,
                          target: undefined,
                          nodejs_script_path: undefined,
                          app_build_command: undefined,
                          app_binary_path: undefined,
                        }));
                      }}
                      aria-pressed={isActive}
                      className={cn(
                        "flex min-h-20 flex-col items-center justify-center gap-2 rounded-xl border px-3 py-3 text-center transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--app-text)]/20",
                        isActive
                          ? "border-[var(--app-accent)] bg-[var(--app-surface-elev)] text-[var(--app-text)] shadow-sm"
                          : "border-[var(--app-border)] bg-[var(--app-surface-elev)] text-[var(--app-text)] hover:bg-[var(--app-surface-elev)]",
                        errors.kind ? "border-[var(--app-danger)]" : "",
                      )}
                    >
                      {imageSrc ? (
                        <img
                          src={imageSrc}
                          alt=""
                          aria-hidden="true"
                          className="h-8 w-8 shrink-0 object-contain"
                        />
                      ) : KindIcon ? (
                        <KindIcon
                          className={cn(
                            "size-8 shrink-0",
                            isActive
                              ? "text-[var(--app-accent)]"
                              : "text-[var(--app-text-muted)]",
                          )}
                        />
                      ) : null}
                      <span className="text-[12px] font-semibold leading-4">
                        {kind}
                      </span>
                    </button>
                  );
                })}
              </div>
              {errors.kind ? (
                <p className="text-[12px] text-[var(--app-danger)]">
                  {errors.kind}
                </p>
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
                  <p className="text-[12px] text-[var(--app-danger)]">
                    {errors.target}
                  </p>
                ) : (
                  <p className="text-[12px] text-[var(--app-text-muted)]">
                    {config.helpText}
                  </p>
                )}
              </div>
            )}

            {isRuntimeDomainKind(form.kind) ? (
              <div className="space-y-2">
                <label
                  htmlFor="domain-nodejs-script-path"
                  className="text-[13px] font-medium text-[var(--app-text)]"
                >
                  Script path
                </label>
                <Input
                  id="domain-nodejs-script-path"
                  value={form.nodeJSScriptPath}
                  onChange={(event) => {
                    setForm((current) => ({
                      ...current,
                      nodeJSScriptPath: event.target.value,
                    }));
                    if (errors.nodejs_script_path) {
                      setErrors((current) => ({
                        ...current,
                        nodejs_script_path: undefined,
                      }));
                    }
                  }}
                  placeholder={getDefaultScriptPath(form.kind)}
                  autoComplete="off"
                  aria-invalid={errors.nodejs_script_path ? "true" : "false"}
                  className={
                    errors.nodejs_script_path ? "border-[var(--app-danger)]" : ""
                  }
                />
                {errors.nodejs_script_path ? (
                  <p className="text-[12px] text-[var(--app-danger)]">
                    {errors.nodejs_script_path}
                  </p>
                ) : (
                  <p className="text-[12px] text-[var(--app-text-muted)]">
                    {form.kind === "Python"
                      ? "Use a path relative to the domain root, for example `app.py` or `src/main.py`."
                      : "Use a path relative to the domain root, for example `bin/www` or `dist/index.js`."}
                  </p>
                )}
              </div>
            ) : null}

            {form.kind === "App" ? (
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <span className="text-[13px] font-medium text-[var(--app-text)]">
                    Build configuration
                  </span>
                  <div className="flex items-center gap-1.5">
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      className="h-7 px-2 text-xs"
                      onClick={() =>
                        applyAppBuildExample(
                          defaultAppBuildCommand,
                          defaultAppBinaryPath,
                        )
                      }
                    >
                      Go example
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      className="h-7 px-2 text-xs"
                      onClick={() =>
                        applyAppBuildExample(
                          "cargo build --release",
                          "target/release/my-app",
                        )
                      }
                    >
                      Rust example
                    </Button>
                  </div>
                </div>
                <div className="space-y-2">
                  <label htmlFor="domain-app-build-command" className="text-[13px] font-medium text-[var(--app-text)]">
                    Build command
                  </label>
                <Textarea
                  id="domain-app-build-command"
                  value={form.appBuildCommand}
                  onChange={(event) => {
                    setForm((current) => ({
                      ...current,
                      appBuildCommand: event.target.value,
                    }));
                    if (errors.app_build_command) {
                      setErrors((current) => ({
                        ...current,
                        app_build_command: undefined,
                      }));
                    }
                  }}
                  placeholder="cargo build --release"
                  autoComplete="off"
                  spellCheck={false}
                  aria-invalid={errors.app_build_command ? "true" : "false"}
                  className={
                    errors.app_build_command
                      ? "min-h-20 resize-y border-[var(--app-danger)] font-mono text-xs"
                      : "min-h-20 resize-y font-mono text-xs"
                  }
                />
                <p
                  className={cn(
                    "text-[12px]",
                    errors.app_build_command
                      ? "text-[var(--app-danger)]"
                      : "text-[var(--app-text-muted)]",
                  )}
                >
                  {errors.app_build_command || "Runs from the repository root after every fetch. The compiler must be installed on the server."}
                </p>
                </div>
                <div className="space-y-2">
                  <label htmlFor="domain-app-binary-path" className="text-[13px] font-medium text-[var(--app-text)]">
                    Executable path
                  </label>
                  <Input
                    id="domain-app-binary-path"
                    value={form.appBinaryPath}
                    onChange={(event) => {
                      setForm((current) => ({
                        ...current,
                        appBinaryPath: event.target.value,
                      }));
                      if (errors.app_binary_path) {
                        setErrors((current) => ({
                          ...current,
                          app_binary_path: undefined,
                        }));
                      }
                    }}
                    placeholder="target/release/my-app"
                    autoComplete="off"
                    spellCheck={false}
                    aria-invalid={errors.app_binary_path ? "true" : "false"}
                    className={cn(
                      "font-mono text-xs",
                      errors.app_binary_path && "border-[var(--app-danger)]",
                    )}
                  />
                  <p className={cn("text-[12px]", errors.app_binary_path ? "text-[var(--app-danger)]" : "text-[var(--app-text-muted)]")}>
                    {errors.app_binary_path || "Path created by the build command, relative to the repository root."}
                  </p>
                </div>
              </div>
            ) : null}

            <DialogFooter className="border-t border-[var(--app-border)] pt-4">
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
