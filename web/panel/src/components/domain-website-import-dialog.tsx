import { useEffect, useMemo, useRef, useState } from "react";
import {
  discoverWebsiteImport,
  getWebsiteImportProfiles,
  importDomainWebsite,
  type DomainApiError,
  type DomainRecord,
  type ImportDomainWebsiteResult,
  type RemotePanelDatabase,
  type WebsiteImportAuthType,
  type WebsiteImportDiscovery,
  type WebsiteImportProfiles,
  type WebsiteImportProvider,
} from "@/api/domains";
import { FieldError } from "@/components/field-error";
import { LoaderCircle } from "@/components/icons/lucide-icons";
import { PasswordInput } from "@/components/password-input";
import { Button } from "@/components/ui/button";
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
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  segmentedTabClassName,
  segmentedTabListClassName,
} from "@/components/ui/tabs";

type ImportForm = {
  provider: WebsiteImportProvider;
  panelHost: string;
  panelPort: string;
  panelUsername: string;
  panelSecret: string;
  authType: WebsiteImportAuthType;
  verifyPanelTLS: boolean;
  siteID: string;
  usePanelBackup: boolean;
  ftpHost: string;
  ftpPort: string;
  ftpUsername: string;
  ftpPassword: string;
  sourcePath: string;
  secureFTP: boolean;
  replaceTargetFiles: boolean;
  importDatabase: boolean;
  databaseID: string;
  sourceDatabaseHost: string;
  sourceDatabasePort: string;
  sourceDatabaseUsername: string;
  sourceDatabasePassword: string;
  destinationDatabaseName: string;
  destinationDatabaseUsername: string;
  destinationDatabasePassword: string;
};

const providerDefaults: Record<
  WebsiteImportProvider,
  Pick<ImportForm, "panelPort" | "authType" | "sourcePath">
> = {
  cpanel: { panelPort: "2083", authType: "token", sourcePath: "public_html" },
  plesk: { panelPort: "8443", authType: "password", sourcePath: "httpdocs" },
};

function generatePassword() {
  const bytes = new Uint8Array(24);
  globalThis.crypto.getRandomValues(bytes);
  return btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

function identifier(value: string, fallback: string) {
  return (
    value
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9_]+/g, "_")
      .replace(/^_+|_+$/g, "") || fallback
  );
}

function initialForm(provider: WebsiteImportProvider = "cpanel"): ImportForm {
  return {
    provider,
    panelHost: "",
    panelUsername: "",
    panelSecret: "",
    verifyPanelTLS: true,
    siteID: "",
    usePanelBackup: provider === "plesk",
    ftpHost: "",
    ftpPort: "21",
    ftpUsername: "",
    ftpPassword: "",
    secureFTP: true,
    replaceTargetFiles: true,
    importDatabase: false,
    databaseID: "",
    sourceDatabaseHost: "",
    sourceDatabasePort: "3306",
    sourceDatabaseUsername: "",
    sourceDatabasePassword: "",
    destinationDatabaseName: "",
    destinationDatabaseUsername: "",
    destinationDatabasePassword: "",
    ...providerDefaults[provider],
  };
}

function formFromProfile(
  provider: WebsiteImportProvider,
  profile?: WebsiteImportProfiles[WebsiteImportProvider],
): ImportForm {
  const form = initialForm(provider);
  return profile
    ? {
        ...form,
        panelHost: profile.host,
        panelPort: String(profile.port),
        panelUsername: profile.username,
        panelSecret: profile.secret,
        authType: profile.auth_type,
        verifyPanelTLS: profile.verify_tls,
      }
    : form;
}

type DomainWebsiteImportDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  domain: DomainRecord;
  onImported: (result: ImportDomainWebsiteResult) => void;
};

export function DomainWebsiteImportDialog({
  open,
  onOpenChange,
  domain,
  onImported,
}: DomainWebsiteImportDialogProps) {
  const [form, setForm] = useState<ImportForm>(initialForm);
  const [profiles, setProfiles] = useState<WebsiteImportProfiles>({});
  const [loadingProfiles, setLoadingProfiles] = useState(false);
  const lastProvider = useRef<WebsiteImportProvider>("cpanel");
  const [discovery, setDiscovery] = useState<WebsiteImportDiscovery | null>(null);
  const [connecting, setConnecting] = useState(false);
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!open) return;
    let active = true;
    setForm(initialForm(lastProvider.current));
    setDiscovery(null);
    setError(null);
    setFieldErrors({});
    setLoadingProfiles(true);
    void getWebsiteImportProfiles(domain.hostname)
      .then((savedProfiles) => {
        if (!active) return;
        setProfiles(savedProfiles);
        const provider = lastProvider.current;
        setForm(formFromProfile(provider, savedProfiles[provider]));
      })
      .catch((caught: DomainApiError) => {
        if (active) setError(caught.message || "Saved panel credentials could not be loaded.");
      })
      .finally(() => {
        if (active) setLoadingProfiles(false);
      });
    return () => {
      active = false;
    };
  }, [domain.hostname, open]);

  const selectedSite = discovery?.sites.find((site) => site.id === form.siteID);
  const associatedDatabases = useMemo(() => {
    if (!discovery) {
      return [];
    }
    const matched = discovery.databases.filter(
      (database) =>
        database.site_id && database.site_id === selectedSite?.subscription_id,
    );
    return matched.length > 0 ? matched : discovery.databases;
  }, [discovery, selectedSite?.subscription_id]);
  const mysqlDatabases = associatedDatabases.filter(
    (database) => database.type === "mysql" || database.type === "mariadb",
  );
  const showingDatabaseFallback =
    associatedDatabases.length > 0 &&
    !associatedDatabases.some(
      (database) =>
        database.site_id && database.site_id === selectedSite?.subscription_id,
    );
  const busy = loadingProfiles || connecting || importing;

  function updateField(field: keyof ImportForm, value: string | boolean) {
    setForm((current) => {
      const next = { ...current, [field]: value };
      if (field === "sourceDatabaseUsername" && !current.destinationDatabaseUsername) {
        next.destinationDatabaseUsername = String(value);
      }
      if (field === "sourceDatabasePassword" && !current.destinationDatabasePassword) {
        next.destinationDatabasePassword = String(value);
      }
      return next;
    });
    setError(null);
    const apiFields: Partial<Record<keyof ImportForm, string>> = {
      panelHost: "host",
      panelPort: "port",
      panelUsername: "username",
      panelSecret: "secret",
      sourcePath: "source_path",
      sourceDatabaseHost: "database.source_host",
      sourceDatabasePort: "database.source_port",
      sourceDatabaseUsername: "database.source_username",
      sourceDatabasePassword: "database.source_password",
      destinationDatabaseName: "database.destination_name",
      destinationDatabaseUsername: "database.destination_username",
      destinationDatabasePassword: "database.destination_password",
    };
    const apiField = apiFields[field];
    if (apiField) {
      setFieldErrors((current) => {
        const next = { ...current };
        delete next[apiField];
        return next;
      });
    }
  }

  function selectProvider(provider: WebsiteImportProvider) {
    lastProvider.current = provider;
    setForm(formFromProfile(provider, profiles[provider]));
    setDiscovery(null);
    setError(null);
    setFieldErrors({});
  }

  function selectSite(siteID: string) {
    const site = discovery?.sites.find((candidate) => candidate.id === siteID);
    setForm((current) => ({
      ...current,
      siteID,
      databaseID: "",
      importDatabase: false,
      ftpHost: current.panelHost,
      ftpUsername: site?.ftp_username || current.ftpUsername,
      sourcePath: site?.document_root || providerDefaults[current.provider].sourcePath,
    }));
    setError(null);
  }

  function selectDatabase(databaseID: string) {
    const database = associatedDatabases.find((candidate) => candidate.id === databaseID);
    const base = identifier(database?.name ?? "", "imported_db");
    setForm((current) => ({
      ...current,
      databaseID,
      sourceDatabaseHost: database?.host || current.panelHost,
      sourceDatabasePort: String(database?.port || 3306),
      sourceDatabaseUsername: database?.username || "",
      destinationDatabaseName: base,
      destinationDatabaseUsername: database?.username || "",
      destinationDatabasePassword: "",
    }));
    setError(null);
  }

  async function handleConnect() {
    if (busy) {
      return;
    }
    setConnecting(true);
    setError(null);
    setFieldErrors({});
    try {
      const profile = {
        provider: form.provider,
        host: form.panelHost.trim(),
        port: Number(form.panelPort),
        username: form.panelUsername.trim(),
        secret: form.panelSecret,
        auth_type: form.authType,
        verify_tls: form.verifyPanelTLS,
      };
      const result = await discoverWebsiteImport(domain.hostname, profile);
      setProfiles((current) => ({ ...current, [form.provider]: profile }));
      setDiscovery(result);
      const site = result.sites[0];
      setForm((current) => ({
        ...current,
        siteID: site?.id ?? "",
        ftpHost: current.panelHost,
        ftpUsername: site?.ftp_username ?? "",
        sourcePath: site?.document_root || providerDefaults[current.provider].sourcePath,
      }));
      if (!site) {
        setError("The panel connection succeeded, but no websites were found.");
      }
    } catch (caught) {
      const connectError = caught as DomainApiError;
      setFieldErrors(connectError.fieldErrors ?? {});
      setError(connectError.message || "Could not connect to the remote panel.");
    } finally {
      setConnecting(false);
    }
  }

  async function handleImport() {
    if (busy || !selectedSite) {
      return;
    }
    const selectedDatabase = associatedDatabases.find(
      (database) => database.id === form.databaseID,
    );
    setImporting(true);
    setError(null);
    setFieldErrors({});
    try {
      const result = await importDomainWebsite(domain.hostname, {
        provider: form.provider,
        use_panel_backup: form.provider === "plesk" && form.usePanelBackup,
        panel:
          form.provider === "plesk" && form.usePanelBackup
            ? {
                provider: "plesk",
                host: form.panelHost.trim(),
                port: Number(form.panelPort),
                username: form.panelUsername.trim(),
                secret: form.panelSecret,
                auth_type: form.authType,
                verify_tls: form.verifyPanelTLS,
              }
            : undefined,
        site_id: selectedSite.id,
        subscription_id: selectedSite.subscription_id,
        site_hostname: selectedSite.hostname,
        host: form.ftpHost.trim(),
        port: Number(form.ftpPort),
        username: form.ftpUsername.trim(),
        password: form.ftpPassword,
        source_path: form.sourcePath.trim(),
        secure: form.secureFTP,
        replace_target_files: form.replaceTargetFiles,
        database:
          form.importDatabase && selectedDatabase
            ? {
                source_name: selectedDatabase.name,
                source_host: form.usePanelBackup
                  ? ""
                  : form.sourceDatabaseHost.trim(),
                source_port: form.usePanelBackup
                  ? 0
                  : Number(form.sourceDatabasePort),
                source_username: form.usePanelBackup
                  ? ""
                  : form.sourceDatabaseUsername.trim(),
                source_password: form.usePanelBackup
                  ? ""
                  : form.sourceDatabasePassword,
                destination_name: form.destinationDatabaseName.trim(),
                destination_username: form.destinationDatabaseUsername.trim(),
                destination_password: form.destinationDatabasePassword,
              }
            : undefined,
      });
      onOpenChange(false);
      onImported(result);
    } catch (caught) {
      const importError = caught as DomainApiError;
      setFieldErrors(importError.fieldErrors ?? {});
      setError(importError.message || "Website could not be imported.");
    } finally {
      setImporting(false);
    }
  }

  const connectDisabled =
    busy ||
    !form.panelHost.trim() ||
    !form.panelPort ||
    !form.panelSecret ||
    (form.authType === "password" && !form.panelUsername.trim()) ||
    (form.provider === "cpanel" && !form.panelUsername.trim());
  const importDisabled =
    busy ||
    !selectedSite ||
    (!form.usePanelBackup &&
      (!form.ftpHost.trim() ||
        !form.ftpPort ||
        !form.ftpUsername.trim() ||
        !form.ftpPassword ||
        !form.sourcePath.trim())) ||
    (form.importDatabase &&
      (!form.databaseID ||
        (!form.usePanelBackup &&
          (!form.sourceDatabaseHost.trim() ||
            !form.sourceDatabasePort ||
            !form.sourceDatabaseUsername.trim() ||
            !form.sourceDatabasePassword)) ||
        !form.destinationDatabaseName.trim() ||
        !form.destinationDatabaseUsername.trim() ||
        form.destinationDatabasePassword.length < 8));

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !busy && onOpenChange(nextOpen)}>
      <DialogContent className="max-h-[calc(100vh-2rem)] gap-4 overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>Import website</DialogTitle>
          <DialogDescription>
            Connect to a hosting panel, select a website and optionally migrate its database into {domain.hostname}.
          </DialogDescription>
        </DialogHeader>

        <div role="tablist" aria-label="Source control panel" className={segmentedTabListClassName}>
          {(["cpanel", "plesk"] as const).map((provider) => (
            <button
              key={provider}
              type="button"
              role="tab"
              aria-selected={form.provider === provider}
              data-active={form.provider === provider}
              className={segmentedTabClassName}
              disabled={busy}
              onClick={() => selectProvider(provider)}
            >
              {provider === "cpanel" ? "cPanel" : "Plesk"}
            </button>
          ))}
        </div>

        {error ? (
          <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-3 text-[13px] text-[var(--app-danger)]">
            {error}
          </div>
        ) : null}

        <section className="space-y-3">
          <div className="text-sm font-medium">Panel API</div>
          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_7rem]">
            <Field label="Panel host" id="website-import-panel-host" error={fieldErrors.host}>
              <Input id="website-import-panel-host" value={form.panelHost} placeholder="panel.example.com" disabled={busy} onChange={(event) => updateField("panelHost", event.target.value)} />
            </Field>
            <Field label="API port" id="website-import-panel-port" error={fieldErrors.port}>
              <Input id="website-import-panel-port" type="number" min={1} max={65535} value={form.panelPort} disabled={busy} onChange={(event) => updateField("panelPort", event.target.value)} />
            </Field>
            <Field label="Authentication" id="website-import-auth-type">
              <Select value={form.authType} disabled={busy} onValueChange={(value: WebsiteImportAuthType) => updateField("authType", value)}>
                <SelectTrigger id="website-import-auth-type"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="token">API token / key</SelectItem>
                  <SelectItem value="password">Panel password</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label={form.provider === "plesk" && form.authType === "token" ? "Username (optional)" : "Panel username"} id="website-import-panel-username" error={fieldErrors.username}>
              <Input id="website-import-panel-username" value={form.panelUsername} autoComplete="username" disabled={busy} onChange={(event) => updateField("panelUsername", event.target.value)} />
            </Field>
            <div className="sm:col-span-2">
              <Field label={form.authType === "token" ? "API token / secret key" : "Panel password"} id="website-import-panel-secret" error={fieldErrors.secret}>
                <PasswordInput id="website-import-panel-secret" value={form.panelSecret} autoComplete="current-password" disabled={busy} onChange={(event) => updateField("panelSecret", event.target.value)} />
              </Field>
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-[var(--app-border)] pt-3">
            <label className="flex items-center gap-2 text-xs text-[var(--app-text-muted)]">
              <Checkbox checked={form.verifyPanelTLS} disabled={busy} onCheckedChange={(checked) => updateField("verifyPanelTLS", checked === true)} />
              Verify panel TLS certificate
            </label>
            <Button type="button" variant="outline" disabled={connectDisabled} onClick={() => void handleConnect()}>
              {connecting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {discovery ? "Reconnect" : "Connect and load sites"}
            </Button>
          </div>
        </section>

        {discovery ? (
          <>
            <section className="space-y-3 border-t border-[var(--app-border)] pt-3">
              <div className="text-sm font-medium">Source website</div>
              <Field label="Website" id="website-import-site">
                <Select value={form.siteID} disabled={busy} onValueChange={selectSite}>
                  <SelectTrigger id="website-import-site"><SelectValue placeholder="Select a website" /></SelectTrigger>
                  <SelectContent>
                    {discovery.sites.map((site) => <SelectItem key={site.id} value={site.id}>{site.hostname}</SelectItem>)}
                  </SelectContent>
                </Select>
              </Field>
              {form.provider === "plesk" ? (
                <div className="space-y-1.5 border-y border-[var(--app-border)] py-2.5">
                  <CheckLabel label="Transfer through Plesk Backup Manager" checked={form.usePanelBackup} disabled={busy} onChange={(checked) => updateField("usePanelBackup", checked)} />
                  <p className="text-xs text-[var(--app-text-muted)]">Uses the panel API for website files and database content. No FTP or source database password is required.</p>
                </div>
              ) : null}
              {!form.usePanelBackup ? (
                <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_7rem]">
                  <Field label="FTP host" id="website-import-ftp-host" error={fieldErrors.host}>
                    <Input id="website-import-ftp-host" value={form.ftpHost} disabled={busy} onChange={(event) => updateField("ftpHost", event.target.value)} />
                  </Field>
                  <Field label="FTP port" id="website-import-ftp-port" error={fieldErrors.port}>
                    <Input id="website-import-ftp-port" type="number" min={1} max={65535} value={form.ftpPort} disabled={busy} onChange={(event) => updateField("ftpPort", event.target.value)} />
                  </Field>
                  <Field label="FTP username" id="website-import-ftp-username" error={fieldErrors.username}>
                    <Input id="website-import-ftp-username" value={form.ftpUsername} autoComplete="username" disabled={busy} onChange={(event) => updateField("ftpUsername", event.target.value)} />
                  </Field>
                  <Field label="FTP password" id="website-import-ftp-password" error={fieldErrors.password}>
                    <PasswordInput id="website-import-ftp-password" value={form.ftpPassword} autoComplete="current-password" disabled={busy} onChange={(event) => updateField("ftpPassword", event.target.value)} />
                  </Field>
                  <div className="sm:col-span-2">
                    <Field label="Source document root" id="website-import-source-path" error={fieldErrors.source_path}>
                      <Input id="website-import-source-path" value={form.sourcePath} disabled={busy} onChange={(event) => updateField("sourcePath", event.target.value)} />
                    </Field>
                  </div>
                </div>
              ) : null}
              <div className="flex flex-wrap gap-x-6 gap-y-2 text-xs text-[var(--app-text-muted)]">
                {!form.usePanelBackup ? <CheckLabel label="Use explicit TLS (FTPS)" checked={form.secureFTP} disabled={busy} onChange={(checked) => updateField("secureFTP", checked)} /> : null}
                <CheckLabel label="Replace current website files" checked={form.replaceTargetFiles} disabled={busy} onChange={(checked) => updateField("replaceTargetFiles", checked)} />
              </div>
            </section>

            <section className="space-y-3 border-t border-[var(--app-border)] pt-3">
              <CheckLabel label="Import a MySQL/MariaDB database" checked={form.importDatabase} disabled={busy || mysqlDatabases.length === 0} onChange={(checked) => updateField("importDatabase", checked)} />
              {mysqlDatabases.length === 0 ? <p className="text-xs text-[var(--app-text-muted)]">No MySQL/MariaDB databases were returned for this account.</p> : null}
              {form.importDatabase ? (
                <div className="space-y-3">
                  <Field label="Source database" id="website-import-database" error={fieldErrors["database.source_name"]}>
                    <Select value={form.databaseID} disabled={busy} onValueChange={selectDatabase}>
                      <SelectTrigger id="website-import-database"><SelectValue placeholder="Select a database" /></SelectTrigger>
                      <SelectContent>
                        {mysqlDatabases.map((database) => <SelectItem key={database.id} value={database.id}>{database.name}</SelectItem>)}
                      </SelectContent>
                    </Select>
                  </Field>
                  {showingDatabaseFallback ? <p className="text-xs text-[var(--app-text-muted)]">No database-to-site association was detected, so all account databases are shown.</p> : null}
                  {!form.usePanelBackup ? (
                    <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_7rem]">
                      <Field label="Database host" id="website-import-db-host" error={fieldErrors["database.source_host"]}>
                        <Input id="website-import-db-host" value={form.sourceDatabaseHost} disabled={busy} onChange={(event) => updateField("sourceDatabaseHost", event.target.value)} />
                      </Field>
                      <Field label="Port" id="website-import-db-port" error={fieldErrors["database.source_port"]}>
                        <Input id="website-import-db-port" type="number" min={1} max={65535} value={form.sourceDatabasePort} disabled={busy} onChange={(event) => updateField("sourceDatabasePort", event.target.value)} />
                      </Field>
                      <Field label="Source DB username" id="website-import-db-username" error={fieldErrors["database.source_username"]}>
                        <Input id="website-import-db-username" value={form.sourceDatabaseUsername} autoComplete="username" disabled={busy} onChange={(event) => updateField("sourceDatabaseUsername", event.target.value)} />
                      </Field>
                      <Field label="Source DB password" id="website-import-db-password" error={fieldErrors["database.source_password"]}>
                        <PasswordInput id="website-import-db-password" value={form.sourceDatabasePassword} autoComplete="current-password" disabled={busy} onChange={(event) => updateField("sourceDatabasePassword", event.target.value)} />
                      </Field>
                    </div>
                  ) : null}
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Field label="Destination database" id="website-import-destination-db" error={fieldErrors["database.destination_name"]}>
                      <Input id="website-import-destination-db" value={form.destinationDatabaseName} disabled={busy} onChange={(event) => updateField("destinationDatabaseName", event.target.value)} />
                    </Field>
                    <Field label="Destination username" id="website-import-destination-user" error={fieldErrors["database.destination_username"]}>
                      <Input id="website-import-destination-user" value={form.destinationDatabaseUsername} disabled={busy} onChange={(event) => updateField("destinationDatabaseUsername", event.target.value)} />
                    </Field>
                    <div className="sm:col-span-2">
                      <Field label="Destination password" id="website-import-destination-password" error={fieldErrors["database.destination_password"]}>
                        <PasswordInput id="website-import-destination-password" value={form.destinationDatabasePassword} disabled={busy} onGeneratePassword={() => updateField("destinationDatabasePassword", generatePassword())} onChange={(event) => updateField("destinationDatabasePassword", event.target.value)} />
                      </Field>
                    </div>
                  </div>
                  <p className="text-xs leading-5 text-[var(--app-text-muted)]">{form.usePanelBackup ? "The selected database dump will be extracted from the temporary Plesk backup." : "The source database must allow this FlowPanel server to connect remotely."} Keep the destination name, username, and password equal to the source values when you want existing application configuration to continue working unchanged.</p>
                </div>
              ) : null}
            </section>
          </>
        ) : null}

        <DialogFooter className="border-t border-[var(--app-border)] pt-3">
          <Button type="button" variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button type="button" disabled={importDisabled} onClick={() => void handleImport()}>
            {importing ? <><LoaderCircle className="h-4 w-4 animate-spin" />Importing</> : "Import website"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, id, error, children }: { label: string; id: string; error?: string; children: React.ReactNode }) {
  return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label>{children}<FieldError message={error} /></div>;
}

function CheckLabel({ label, checked, disabled, onChange }: { label: string; checked: boolean; disabled: boolean; onChange: (checked: boolean) => void }) {
  return <label className="flex items-center gap-2"><Checkbox checked={checked} disabled={disabled} onCheckedChange={(value) => onChange(value === true)} />{label}</label>;
}
