import { useEffect, useRef, useState } from "react";
import {
  fetchDomainWordPressStatus,
  installDomainWordPress,
  installDomainWordPressPlugin,
  installDomainWordPressTheme,
  runDomainWordPressPluginAction,
  runDomainWordPressThemeAction,
  updateDomainWordPressCore,
  type WordPressApiError,
  type WordPressExtension,
  type WordPressInstallExtensionInput,
  type WordPressInstallInput,
  type WordPressStatus,
} from "@/api/domain-wordpress";
import { type DomainRecord } from "@/api/domains";
import {
  BrandWordpress,
  LoaderCircle,
  Package,
  Palette,
  RefreshCw,
  Settings2,
} from "@/components/icons/tabler-icons";
import { PasswordInput } from "@/components/password-input";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { toast } from "sonner";

type DomainWordPressDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  domain: DomainRecord | null;
};

type InstallFormState = WordPressInstallInput;

const initialExtensionInstallForm: WordPressInstallExtensionInput = {
  slug: "",
  activate: false,
};

const generatedPasswordLength = 20;
const wordPressInstallDirectoryDirtyMessage =
  "document root is not empty, so WordPress installation was refused";

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

function createInstallForm(hostname: string): InstallFormState {
  return {
    database_name: "",
    site_url: hostname ? `https://${hostname}` : "",
    site_title: hostname,
    admin_username: "admin",
    admin_email: hostname ? `admin@${hostname}` : "",
    admin_password: "",
    table_prefix: "wp_",
  };
}

function generateWordPressPassword(length = generatedPasswordLength) {
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
  const randomBytes = new Uint8Array(length);

  if (globalThis.crypto?.getRandomValues) {
    globalThis.crypto.getRandomValues(randomBytes);
  } else {
    for (let index = 0; index < randomBytes.length; index += 1) {
      randomBytes[index] = Math.floor(Math.random() * 256);
    }
  }

  return Array.from(
    randomBytes,
    (value) => alphabet[value % alphabet.length],
  ).join("");
}

function getExtensionLabel(extension: WordPressExtension) {
  return extension.title?.trim() || extension.name;
}

function isPluginActive(status?: string) {
  return status === "active" || status === "active-network";
}

function canDeletePlugin(status?: string) {
  return (
    status !== "active" &&
    status !== "active-network" &&
    status !== "must-use" &&
    status !== "dropin"
  );
}

function canDeleteTheme(status?: string) {
  return status !== "active";
}

function getActionSuccessLabel(action: "activate" | "deactivate" | "delete" | "update") {
  switch (action) {
    case "activate":
      return "activated";
    case "deactivate":
      return "deactivated";
    case "delete":
      return "deleted";
    case "update":
      return "updated";
  }
}

function WordPressExtensionsTable({
  type,
  items,
  busy,
  runningAction,
  onAction,
}: {
  type: "plugin" | "theme";
  items: WordPressExtension[];
  busy: boolean;
  runningAction: string | null;
  onAction: (
    name: string,
    action: "activate" | "deactivate" | "delete" | "update",
  ) => void;
}) {
  if (items.length === 0) {
    return (
      <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-4 text-[13px] text-[var(--app-text-muted)]">
        No WordPress {type}s were detected for this site.
      </div>
    );
  }

  return (
    <div className="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead>{type === "plugin" ? "Plugin" : "Theme"}</TableHead>
            <TableHead className="w-[140px]">Status</TableHead>
            <TableHead className="w-[140px]">Version</TableHead>
            <TableHead className="w-[220px] text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((item) => {
            const active = isPluginActive(item.status);
            const canActivate = type === "plugin" ? item.status === "inactive" : item.status !== "active";
            const canDeactivate = type === "plugin" && active;
            const canDelete = type === "plugin" ? canDeletePlugin(item.status) : canDeleteTheme(item.status);
            const canUpdate = item.update === "available";

            return (
              <TableRow key={item.name}>
                <TableCell className="max-w-0">
                  <div className="space-y-1">
                    <div className="truncate font-medium text-[var(--app-text)]" title={item.name}>
                      {getExtensionLabel(item)}
                    </div>
                    <div className="truncate font-mono text-[12px] text-[var(--app-text-muted)]" title={item.name}>
                      {item.name}
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-2">
                    {item.status ? <Badge variant="outline">{item.status}</Badge> : null}
                    {item.update === "available" ? (
                      <Badge variant="outline">
                        Update {item.update_version || "available"}
                      </Badge>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell className="font-mono text-[13px] text-[var(--app-text-muted)]">
                  {item.version || "Unknown"}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex flex-wrap justify-end gap-2">
                    {canUpdate ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        disabled={busy}
                        onClick={() => onAction(item.name, "update")}
                      >
                        {runningAction === `${type}:update:${item.name}` ? "Updating..." : "Update"}
                      </Button>
                    ) : null}
                    {canActivate ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        disabled={busy}
                        onClick={() => onAction(item.name, "activate")}
                      >
                        {runningAction === `${type}:activate:${item.name}` ? "Activating..." : "Activate"}
                      </Button>
                    ) : null}
                    {canDeactivate ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        disabled={busy}
                        onClick={() => onAction(item.name, "deactivate")}
                      >
                        {runningAction === `${type}:deactivate:${item.name}` ? "Deactivating..." : "Deactivate"}
                      </Button>
                    ) : null}
                    {canDelete ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        disabled={busy}
                        onClick={() => onAction(item.name, "delete")}
                      >
                        {runningAction === `${type}:delete:${item.name}` ? "Deleting..." : "Delete"}
                      </Button>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

export function DomainWordPressDialog({
  open,
  onOpenChange,
  domain,
}: DomainWordPressDialogProps) {
  const activeRef = useRef(false);
  const [status, setStatus] = useState<WordPressStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [runningAction, setRunningAction] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [installFieldErrors, setInstallFieldErrors] = useState<Record<string, string>>(
    {},
  );
  const [pluginFieldErrors, setPluginFieldErrors] = useState<Record<string, string>>(
    {},
  );
  const [themeFieldErrors, setThemeFieldErrors] = useState<Record<string, string>>(
    {},
  );
  const [confirmClearRootOpen, setConfirmClearRootOpen] = useState(false);
  const [installForm, setInstallForm] = useState<InstallFormState>(
    createInstallForm(""),
  );
  const [pluginForm, setPluginForm] = useState<WordPressInstallExtensionInput>({
    ...initialExtensionInstallForm,
    activate: true,
  });
  const [themeForm, setThemeForm] = useState<WordPressInstallExtensionInput>({
    ...initialExtensionInstallForm,
  });

  async function loadStatus(mode: "initial" | "refresh" = "refresh") {
    if (!domain) {
      return;
    }

    if (mode === "initial") {
      setLoading(true);
    } else {
      setRefreshing(true);
    }

    try {
      const nextStatus = await fetchDomainWordPressStatus(domain.hostname);
      if (!activeRef.current) {
        return;
      }

      setStatus(nextStatus);
      setInstallForm((current) => (
        current.database_name || nextStatus.installed
          ? current
          : {
              ...current,
              database_name: nextStatus.suggested_database_name ?? current.database_name,
            }
      ));
      setError(null);
    } catch (loadError) {
      if (!activeRef.current) {
        return;
      }

      setStatus(null);
      setError(getErrorMessage(loadError, "Failed to load WordPress toolkit."));
    } finally {
      if (!activeRef.current) {
        return;
      }

      setLoading(false);
      setRefreshing(false);
    }
  }

  useEffect(() => {
    if (!open || !domain) {
      activeRef.current = false;
      setStatus(null);
      setLoading(false);
      setRefreshing(false);
      setInstalling(false);
      setRunningAction(null);
      setError(null);
      setInstallFieldErrors({});
      setPluginFieldErrors({});
      setThemeFieldErrors({});
      setConfirmClearRootOpen(false);
      setInstallForm(createInstallForm(domain?.hostname ?? ""));
      setPluginForm({ ...initialExtensionInstallForm, activate: true });
      setThemeForm({ ...initialExtensionInstallForm });
      return;
    }

    activeRef.current = true;
    setStatus(null);
    setLoading(false);
    setRefreshing(false);
    setInstalling(false);
    setRunningAction(null);
    setError(null);
    setInstallFieldErrors({});
    setPluginFieldErrors({});
    setThemeFieldErrors({});
    setConfirmClearRootOpen(false);
    setInstallForm(createInstallForm(domain.hostname));
    setPluginForm({ ...initialExtensionInstallForm, activate: true });
    setThemeForm({ ...initialExtensionInstallForm });
    void loadStatus("initial");

    return () => {
      activeRef.current = false;
    };
  }, [domain, open]);

  const busy = loading || refreshing || installing || runningAction !== null;

  async function handleInstallWordPress(clearDocumentRoot = false) {
    if (!domain) {
      return;
    }

    setInstalling(true);
    setError(null);
    setInstallFieldErrors({});
    if (clearDocumentRoot) {
      setConfirmClearRootOpen(false);
    }

    try {
      const nextStatus = await installDomainWordPress(domain.hostname, {
        ...installForm,
        clear_document_root: clearDocumentRoot || undefined,
      });
      if (!activeRef.current) {
        return;
      }

      setStatus(nextStatus);
      toast.success(`WordPress installed for ${domain.hostname}.`);
    } catch (installError) {
      if (!activeRef.current) {
        return;
      }

      const nextError = installError as WordPressApiError;
      if (
        !clearDocumentRoot &&
        nextError.message === wordPressInstallDirectoryDirtyMessage
      ) {
        setConfirmClearRootOpen(true);
        return;
      }
      setInstallFieldErrors(nextError.fieldErrors ?? {});
      const message = nextError.message || "WordPress could not be installed.";
      setError(message);
      toast.error(message);
    } finally {
      if (activeRef.current) {
        setInstalling(false);
      }
    }
  }

  async function runStatusMutation(
    actionKey: string,
    request: () => Promise<WordPressStatus>,
    successMessage: string,
    onValidationError?: (fieldErrors: Record<string, string>) => void,
    onSuccess?: () => void,
  ) {
    setRunningAction(actionKey);
    setError(null);

    try {
      const nextStatus = await request();
      if (!activeRef.current) {
        return;
      }

      setStatus(nextStatus);
      onSuccess?.();
      toast.success(successMessage);
    } catch (mutationError) {
      if (!activeRef.current) {
        return;
      }

      const nextError = mutationError as WordPressApiError;
      onValidationError?.(nextError.fieldErrors ?? {});
      const message = nextError.message || "WordPress action failed.";
      setError(message);
      toast.error(message);
    } finally {
      if (activeRef.current) {
        setRunningAction(null);
      }
    }
  }

  async function handleWordPressCoreUpdate() {
    if (!domain) {
      return;
    }

    await runStatusMutation(
      "core:update",
      () => updateDomainWordPressCore(domain.hostname),
      `Updated WordPress core for ${domain.hostname}.`,
    );
  }

  async function handlePluginInstall() {
    if (!domain) {
      return;
    }

    setPluginFieldErrors({});
    await runStatusMutation(
      "plugin:install",
      () => installDomainWordPressPlugin(domain.hostname, pluginForm),
      `Installed plugin ${pluginForm.slug}.`,
      (fieldErrors) => {
        setPluginFieldErrors(fieldErrors);
      },
      () => {
        setPluginForm((current) => ({
          ...current,
          slug: "",
        }));
      },
    );
  }

  async function handleThemeInstall() {
    if (!domain) {
      return;
    }

    setThemeFieldErrors({});
    await runStatusMutation(
      "theme:install",
      () => installDomainWordPressTheme(domain.hostname, themeForm),
      `Installed theme ${themeForm.slug}.`,
      (fieldErrors) => {
        setThemeFieldErrors(fieldErrors);
      },
      () => {
        setThemeForm((current) => ({
          ...current,
          slug: "",
        }));
      },
    );
  }

  async function handlePluginAction(
    name: string,
    action: "activate" | "deactivate" | "delete" | "update",
  ) {
    if (!domain) {
      return;
    }

    await runStatusMutation(
      `plugin:${action}:${name}`,
      () => runDomainWordPressPluginAction(domain.hostname, { name, action }),
      `Plugin ${name} ${getActionSuccessLabel(action)}.`,
    );
  }

  async function handleThemeAction(
    name: string,
    action: "activate" | "deactivate" | "delete" | "update",
  ) {
    if (!domain) {
      return;
    }

    await runStatusMutation(
      `theme:${action}:${name}`,
      () => runDomainWordPressThemeAction(domain.hostname, { name, action }),
      `Theme ${name} ${getActionSuccessLabel(action)}.`,
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-4 sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>{domain?.hostname ?? "Domain"} WordPress</DialogTitle>
          <DialogDescription>
            Use WP-CLI to install WordPress and manage core, plugins, and themes for this PHP site.
          </DialogDescription>
        </DialogHeader>

        <div className="max-h-[80vh] space-y-4 overflow-y-auto pr-1">
          {error ? (
            <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
              {error}
            </div>
          ) : null}

          <section className="grid gap-3 md:grid-cols-2">
            <div className="rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
              <div className="text-xs text-[var(--app-text-muted)]">Document root</div>
              <div className="mt-2 break-all font-mono text-[13px] text-[var(--app-text)]">
                {status?.document_root || "Loading..."}
              </div>
            </div>
            <div className="rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
              <div className="text-xs text-[var(--app-text-muted)]">WordPress</div>
              <div className="mt-2 flex items-center gap-2">
                <Badge variant="outline">
                  {status?.installed ? "Installed" : "Not installed"}
                </Badge>
                {status?.version ? (
                  <span className="text-sm font-medium text-[var(--app-text)]">
                    {status.version}
                  </span>
                ) : null}
              </div>
              <div className="mt-2 text-[12px] text-[var(--app-text-muted)]">
                {status?.site_url || "Install WordPress to enable plugin, theme, and update management."}
              </div>
            </div>
          </section>

          <div className="flex justify-end">
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={busy || !domain}
              onClick={() => {
                void loadStatus("refresh");
              }}
            >
              {refreshing ? (
                <>
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  Refreshing...
                </>
              ) : (
                <>
                  <RefreshCw className="h-4 w-4" />
                  Refresh
                </>
              )}
            </Button>
          </div>

          {loading ? (
            <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-4 text-[13px] text-[var(--app-text-muted)]">
              Loading WordPress toolkit...
            </div>
          ) : null}

          {!loading && status?.inspect_error ? (
            <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
              {status.inspect_error}
            </div>
          ) : null}

          {!loading && status && !status.installed ? (
            <section className="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
              <div className="space-y-1">
                <div className="flex items-center gap-2 text-sm font-semibold text-[var(--app-text)]">
                  <BrandWordpress className="h-4 w-4 text-[var(--app-text-muted)]" stroke={1.8} />
                  <span>Install WordPress</span>
                </div>
                <p className="text-[13px] leading-6 text-[var(--app-text-muted)]">
                  FlowPanel will use WP-CLI to download WordPress, create a MariaDB database with an auto-generated user and password, create the config if needed, and run <code>wp core install</code>.
                </p>
                {status.core_files_present || status.config_present ? (
                  <p className="text-[13px] leading-6 text-[var(--app-text-muted)]">
                    Existing WordPress files or a <code>wp-config.php</code> were detected. Installation will reuse them unless the document root needs to be cleared first.
                  </p>
                ) : null}
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                {status.config_present ? (
                  <div className="space-y-2">
                    <Label>Database</Label>
                    <div className="rounded-md border border-[var(--app-border)] bg-[var(--app-surface)] px-3 py-2 text-[13px] leading-5 text-[var(--app-text-muted)]">
                      Existing <code>wp-config.php</code> settings will be reused, so no new database will be created for this install.
                    </div>
                  </div>
                ) : (
                  <div className="space-y-2">
                    <Label htmlFor="wordpress_database_name">Database name</Label>
                    <Input
                      id="wordpress_database_name"
                      value={installForm.database_name}
                      disabled={busy}
                      placeholder={status.suggested_database_name || undefined}
                      onChange={(event) => {
                        setInstallFieldErrors((current) => {
                          const next = { ...current };
                          delete next.database_name;
                          return next;
                        });
                        setInstallForm((current) => ({
                          ...current,
                          database_name: event.target.value,
                        }));
                      }}
                    />
                    <p className="text-[12px] leading-5 text-[var(--app-text-muted)]">
                      FlowPanel will create this database, generate a user and password automatically, and link it to this domain.
                    </p>
                    {installFieldErrors.database_name ? (
                      <p className="text-[12px] text-[var(--app-danger)]">
                        {installFieldErrors.database_name}
                      </p>
                    ) : null}
                  </div>
                )}

                <div className="space-y-2">
                  <Label htmlFor="wordpress_table_prefix">Table prefix</Label>
                  <Input
                    id="wordpress_table_prefix"
                    value={installForm.table_prefix}
                    disabled={busy}
                    onChange={(event) => {
                      setInstallFieldErrors((current) => {
                        const next = { ...current };
                        delete next.table_prefix;
                        return next;
                      });
                      setInstallForm((current) => ({
                        ...current,
                        table_prefix: event.target.value,
                      }));
                    }}
                  />
                  {installFieldErrors.table_prefix ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {installFieldErrors.table_prefix}
                    </p>
                  ) : null}
                </div>

                <div className="space-y-2 md:col-span-2">
                  <Label htmlFor="wordpress_site_url">Site URL</Label>
                  <Input
                    id="wordpress_site_url"
                    value={installForm.site_url}
                    disabled={busy}
                    onChange={(event) => {
                      setInstallFieldErrors((current) => {
                        const next = { ...current };
                        delete next.site_url;
                        return next;
                      });
                      setInstallForm((current) => ({
                        ...current,
                        site_url: event.target.value,
                      }));
                    }}
                  />
                  {installFieldErrors.site_url ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {installFieldErrors.site_url}
                    </p>
                  ) : null}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="wordpress_site_title">Site title</Label>
                  <Input
                    id="wordpress_site_title"
                    value={installForm.site_title}
                    disabled={busy}
                    onChange={(event) => {
                      setInstallFieldErrors((current) => {
                        const next = { ...current };
                        delete next.site_title;
                        return next;
                      });
                      setInstallForm((current) => ({
                        ...current,
                        site_title: event.target.value,
                      }));
                    }}
                  />
                  {installFieldErrors.site_title ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {installFieldErrors.site_title}
                    </p>
                  ) : null}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="wordpress_admin_username">Admin username</Label>
                  <Input
                    id="wordpress_admin_username"
                    value={installForm.admin_username}
                    disabled={busy}
                    onChange={(event) => {
                      setInstallFieldErrors((current) => {
                        const next = { ...current };
                        delete next.admin_username;
                        return next;
                      });
                      setInstallForm((current) => ({
                        ...current,
                        admin_username: event.target.value,
                      }));
                    }}
                  />
                  {installFieldErrors.admin_username ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {installFieldErrors.admin_username}
                    </p>
                  ) : null}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="wordpress_admin_email">Admin email</Label>
                  <Input
                    id="wordpress_admin_email"
                    type="email"
                    value={installForm.admin_email}
                    disabled={busy}
                    onChange={(event) => {
                      setInstallFieldErrors((current) => {
                        const next = { ...current };
                        delete next.admin_email;
                        return next;
                      });
                      setInstallForm((current) => ({
                        ...current,
                        admin_email: event.target.value,
                      }));
                    }}
                  />
                  {installFieldErrors.admin_email ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {installFieldErrors.admin_email}
                    </p>
                  ) : null}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="wordpress_admin_password">Admin password</Label>
                  <PasswordInput
                    id="wordpress_admin_password"
                    value={installForm.admin_password}
                    disabled={busy}
                    onChange={(event) => {
                      setInstallFieldErrors((current) => {
                        const next = { ...current };
                        delete next.admin_password;
                        return next;
                      });
                      setInstallForm((current) => ({
                        ...current,
                        admin_password: event.target.value,
                      }));
                    }}
                    onGeneratePassword={() => {
                      setInstallFieldErrors((current) => {
                        const next = { ...current };
                        delete next.admin_password;
                        return next;
                      });
                      setInstallForm((current) => ({
                        ...current,
                        admin_password: generateWordPressPassword(),
                      }));
                    }}
                  />
                  {installFieldErrors.admin_password ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {installFieldErrors.admin_password}
                    </p>
                  ) : null}
                </div>
              </div>

              <div className="flex justify-end">
                <Button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    void handleInstallWordPress();
                  }}
                >
                  {installing ? "Installing..." : "Install WordPress"}
                </Button>
              </div>
            </section>
          ) : null}

          {!loading && status?.cli_available && status.installed ? (
            <>
              <section className="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2 text-sm font-semibold text-[var(--app-text)]">
                      <Settings2 className="h-4 w-4 text-[var(--app-text-muted)]" stroke={1.8} />
                      <span>Core</span>
                    </div>
                    <div className="text-[13px] text-[var(--app-text-muted)]">
                      {status.site_title || domain?.hostname}
                    </div>
                    <div className="break-all text-[13px] text-[var(--app-text-muted)]">
                      {status.site_url}
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="outline">WordPress {status.version || "Unknown"}</Badge>
                    {status.core_update?.version ? (
                      <Badge variant="outline">
                        Update {status.core_update.version}
                      </Badge>
                    ) : null}
                    <Button
                      type="button"
                      size="sm"
                      disabled={busy}
                      onClick={() => {
                        void handleWordPressCoreUpdate();
                      }}
                    >
                      {runningAction === "core:update"
                        ? "Updating..."
                        : status.core_update?.version
                          ? "Update core"
                          : "Run core update"}
                    </Button>
                  </div>
                </div>
              </section>

              <section className="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
                <div className="flex items-center gap-2 text-sm font-semibold text-[var(--app-text)]">
                  <Package className="h-4 w-4 text-[var(--app-text-muted)]" stroke={1.8} />
                  <span>Plugins</span>
                </div>

                <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_auto_auto] md:items-end">
                  <div className="space-y-2">
                    <Label htmlFor="wordpress_plugin_slug">Plugin slug</Label>
                    <Input
                      id="wordpress_plugin_slug"
                      value={pluginForm.slug}
                      disabled={busy}
                      placeholder="woocommerce"
                      onChange={(event) => {
                        setPluginFieldErrors((current) => {
                          const next = { ...current };
                          delete next.slug;
                          return next;
                        });
                        setPluginForm((current) => ({
                          ...current,
                          slug: event.target.value,
                        }));
                      }}
                    />
                    {pluginFieldErrors.slug ? (
                      <p className="text-[12px] text-[var(--app-danger)]">
                        {pluginFieldErrors.slug}
                      </p>
                    ) : null}
                  </div>
                  <div className="flex items-center gap-3 pb-1">
                    <Switch
                      id="wordpress_plugin_activate"
                      checked={pluginForm.activate}
                      disabled={busy}
                      onCheckedChange={(checked) => {
                        setPluginForm((current) => ({
                          ...current,
                          activate: checked,
                        }));
                      }}
                    />
                    <Label htmlFor="wordpress_plugin_activate">Activate after install</Label>
                  </div>
                  <Button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      void handlePluginInstall();
                    }}
                  >
                    {runningAction === "plugin:install" ? "Installing..." : "Install plugin"}
                  </Button>
                </div>

                <WordPressExtensionsTable
                  type="plugin"
                  items={status.plugins}
                  busy={busy}
                  runningAction={runningAction}
                  onAction={(name, action) => {
                    void handlePluginAction(name, action);
                  }}
                />
              </section>

              <section className="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
                <div className="flex items-center gap-2 text-sm font-semibold text-[var(--app-text)]">
                  <Palette className="h-4 w-4 text-[var(--app-text-muted)]" stroke={1.8} />
                  <span>Themes</span>
                </div>

                <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_auto_auto] md:items-end">
                  <div className="space-y-2">
                    <Label htmlFor="wordpress_theme_slug">Theme slug</Label>
                    <Input
                      id="wordpress_theme_slug"
                      value={themeForm.slug}
                      disabled={busy}
                      placeholder="astra"
                      onChange={(event) => {
                        setThemeFieldErrors((current) => {
                          const next = { ...current };
                          delete next.slug;
                          return next;
                        });
                        setThemeForm((current) => ({
                          ...current,
                          slug: event.target.value,
                        }));
                      }}
                    />
                    {themeFieldErrors.slug ? (
                      <p className="text-[12px] text-[var(--app-danger)]">
                        {themeFieldErrors.slug}
                      </p>
                    ) : null}
                  </div>
                  <div className="flex items-center gap-3 pb-1">
                    <Switch
                      id="wordpress_theme_activate"
                      checked={themeForm.activate}
                      disabled={busy}
                      onCheckedChange={(checked) => {
                        setThemeForm((current) => ({
                          ...current,
                          activate: checked,
                        }));
                      }}
                    />
                    <Label htmlFor="wordpress_theme_activate">Activate after install</Label>
                  </div>
                  <Button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      void handleThemeInstall();
                    }}
                  >
                    {runningAction === "theme:install" ? "Installing..." : "Install theme"}
                  </Button>
                </div>

                <WordPressExtensionsTable
                  type="theme"
                  items={status.themes}
                  busy={busy}
                  runningAction={runningAction}
                  onAction={(name, action) => {
                    void handleThemeAction(name, action);
                  }}
                />
              </section>
            </>
          ) : null}
        </div>
      </DialogContent>
      <ActionConfirmDialog
        open={confirmClearRootOpen}
        onOpenChange={(nextOpen) => {
          if (installing) {
            return;
          }
          setConfirmClearRootOpen(nextOpen);
        }}
        title="Delete website root?"
        desc="The document root is not empty. Continuing will delete the current website root contents before WordPress is installed."
        confirmText={installing ? "Deleting root..." : "Delete and install"}
        cancelBtnText="Cancel"
        destructive
        isLoading={installing}
        handleConfirm={() => {
          void handleInstallWordPress(true);
        }}
      />
    </Dialog>
  );
}
