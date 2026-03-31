import {
  useDeferredValue,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  createMariaDBDatabase,
  deleteMariaDBDatabase,
  fetchMariaDBDatabases,
  fetchMariaDBRootPassword,
  fetchMariaDBStatus,
  updateMariaDBRootPassword,
  updateMariaDBDatabase,
  type CreateMariaDBDatabaseInput,
  type MariaDBApiError,
  type MariaDBDatabase,
} from "@/api/mariadb";
import { fetchDomains, type DomainRecord } from "@/api/domains";
import { Check, Copy, Eye, EyeOff, Pencil, Plus, RefreshCw, Search, Trash2 } from "@/components/icons/tabler-icons";
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
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

type DialogMode = "create" | "edit" | null;

type FormState = {
  name: string;
  currentUsername: string;
  username: string;
  password: string;
  domain: string;
};

type FormErrors = {
  name?: string;
  username?: string;
  current_username?: string;
  password?: string;
  domain?: string;
};

const initialForm: FormState = {
  name: "",
  currentUsername: "",
  username: "",
  password: "",
  domain: "",
};

function normalizeIdentifier(value: string) {
  return value.trim();
}

function validateIdentifier(value: string, label: string) {
  if (!value) {
    return `${label} is required.`;
  }
  if (!/^[A-Za-z0-9_]+$/.test(value)) {
    return `${label} can contain only letters, numbers, and underscores.`;
  }

  return undefined;
}

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

function generateRootPassword() {
  const randomBytes = new Uint8Array(24);
  window.crypto.getRandomValues(randomBytes);

  let binary = "";
  for (const value of randomBytes) {
    binary += String.fromCharCode(value);
  }

  return window.btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function getDatabasePasswordKey(database: Pick<MariaDBDatabase, "name" | "username">) {
  return `${database.name}:${database.username}`;
}

function maskPassword(password: string) {
  return password ? "**********" : "";
}

function formatStatusSummary(product?: string, version?: string) {
  const normalizedProduct = product?.trim() || "MySQL / MariaDB";
  const normalizedVersion = version?.trim();

  if (!normalizedVersion) {
    return normalizedProduct;
  }

  const versionMatch = normalizedVersion.match(/\b\d+(?:\.\d+)+\b/);
  if (versionMatch) {
    return `${normalizedProduct} ${versionMatch[0]}`;
  }

  return `${normalizedProduct} ${normalizedVersion}`;
}

function ToolbarButton({
  children,
  disabled = false,
}: {
  children: ReactNode;
  disabled?: boolean;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      disabled={disabled}
      className="h-10 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 text-[13px] font-medium text-[var(--app-text)] disabled:opacity-80"
    >
      {children}
    </Button>
  );
}

export function DatabasePage() {
  const [databases, setDatabases] = useState<MariaDBDatabase[]>([]);
  const [domains, setDomains] = useState<DomainRecord[]>([]);
  const [statusSummary, setStatusSummary] = useState("MySQL / MariaDB");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [domainsLoadError, setDomainsLoadError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [dialogMode, setDialogMode] = useState<DialogMode>(null);
  const [form, setForm] = useState<FormState>(initialForm);
  const [errors, setErrors] = useState<FormErrors>({});
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [deletingName, setDeletingName] = useState<string | null>(null);
  const [rootPasswordOpen, setRootPasswordOpen] = useState(false);
  const [rootPassword, setRootPassword] = useState<string>("");
  const [rootPasswordDraft, setRootPasswordDraft] = useState<string>("");
  const [rootPasswordConfigured, setRootPasswordConfigured] = useState(false);
  const [rootPasswordLoading, setRootPasswordLoading] = useState(false);
  const [rootPasswordSaving, setRootPasswordSaving] = useState(false);
  const [rootPasswordError, setRootPasswordError] = useState<string | null>(null);
  const [visiblePasswords, setVisiblePasswords] = useState<Record<string, boolean>>({});
  const [copiedPasswordKey, setCopiedPasswordKey] = useState<string | null>(null);
  const nameInputRef = useRef<HTMLInputElement | null>(null);
  const deferredSearch = useDeferredValue(search);

  useEffect(() => {
    let active = true;

    async function loadData() {
      try {
        const [databasesResult, statusResult, domainsResult] = await Promise.allSettled([
          fetchMariaDBDatabases(),
          fetchMariaDBStatus(),
          fetchDomains(),
        ]);

        if (!active) {
          return;
        }

        if (databasesResult.status === "fulfilled") {
          setDatabases(databasesResult.value.databases);
          setLoadError(null);
        } else {
          setLoadError(getErrorMessage(databasesResult.reason, "Failed to load databases."));
        }

        if (statusResult.status === "fulfilled") {
          setStatusSummary(formatStatusSummary(statusResult.value.product, statusResult.value.version));
        } else {
          setStatusSummary("MySQL / MariaDB");
        }

        if (domainsResult.status === "fulfilled") {
          setDomains(domainsResult.value.domains);
          setDomainsLoadError(null);
        } else {
          setDomains([]);
          setDomainsLoadError(getErrorMessage(domainsResult.reason, "Failed to load domains."));
        }
      } finally {
        if (active) {
          setLoading(false);
        }
      }
    }

    void loadData();

    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!rootPasswordOpen) {
      return;
    }

    let active = true;

    async function loadRootPassword() {
      setRootPasswordLoading(true);
      setRootPasswordError(null);

      try {
        const payload = await fetchMariaDBRootPassword();
        if (!active) {
          return;
        }

        setRootPassword(payload.root_password);
        setRootPasswordDraft(payload.root_password);
        setRootPasswordConfigured(payload.configured);
      } catch (error) {
        if (!active) {
          return;
        }
        setRootPasswordError(getErrorMessage(error, "Failed to load MariaDB root password."));
      } finally {
        if (active) {
          setRootPasswordLoading(false);
        }
      }
    }

    void loadRootPassword();

    return () => {
      active = false;
    };
  }, [rootPasswordOpen]);

  const rootPasswordDirty = rootPasswordDraft !== rootPassword;
  const rootPasswordCandidate = rootPasswordDraft.trim();
  const rootPasswordTooShort = rootPasswordCandidate.length > 0 && rootPasswordCandidate.length < 8;

  function handleGenerateRootPassword() {
    setRootPasswordDraft(generateRootPassword());
    setRootPasswordError(null);
  }

  function handleGenerateDatabasePassword() {
    setForm((current) => ({ ...current, password: generateRootPassword() }));
    setErrors((current) => ({ ...current, password: undefined }));
  }

  function handleCancelRootPasswordEdit() {
    setRootPasswordDraft(rootPassword);
    setRootPasswordError(null);
    setRootPasswordOpen(false);
  }

  async function handleSaveRootPassword() {
    const nextPassword = rootPasswordDraft.trim();
    if (nextPassword.length < 8) {
      setRootPasswordError("Password must be at least 8 characters.");
      return;
    }

    setRootPasswordSaving(true);
    setRootPasswordError(null);

    try {
      const payload = await updateMariaDBRootPassword(nextPassword);
      setRootPassword(payload.root_password);
      setRootPasswordDraft(payload.root_password);
      setRootPasswordConfigured(payload.configured);
    } catch (error) {
      setRootPasswordError(getErrorMessage(error, "Failed to update MariaDB root password."));
    } finally {
      setRootPasswordSaving(false);
    }
  }

  const normalizedSearch = deferredSearch.trim().toLowerCase();
  const filteredDatabases = databases.filter((database) => {
    if (!normalizedSearch) {
      return true;
    }

    return `${database.name} ${database.username} ${database.host} ${database.password ?? ""} ${database.domain ?? ""}`
      .toLowerCase()
      .includes(normalizedSearch);
  });

  const formTitle = dialogMode === "create" ? "Create database" : "Edit database";
  const formDescription =
    dialogMode === "create"
      ? "Create a database, assign credentials, and optionally link a domain."
      : "Update the linked username, domain, or password. Leave password blank to keep it unchanged.";
  const selectedDomainMissing =
    form.domain !== "" && !domains.some((domain) => domain.hostname === form.domain);

  function resetForm() {
    setForm(initialForm);
    setErrors({});
    setFormError(null);
    setSubmitting(false);
  }

  function openCreateDialog() {
    resetForm();
    setDialogMode("create");
  }

  function openEditDialog(database: MariaDBDatabase) {
    resetForm();
    setForm({
      name: database.name,
      currentUsername: database.username,
      username: database.username,
      password: "",
      domain: database.domain ?? "",
    });
    setDialogMode("edit");
  }

  function closeDialog() {
    if (submitting) {
      return;
    }

    setDialogMode(null);
    resetForm();
  }

  async function reloadDatabases() {
    const payload = await fetchMariaDBDatabases();
    setDatabases(payload.databases);
    setLoadError(null);
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!dialogMode) {
      return;
    }

    const nextForm: FormState = {
      name: normalizeIdentifier(form.name),
      currentUsername: normalizeIdentifier(form.currentUsername),
      username: normalizeIdentifier(form.username),
      password: form.password.trim(),
      domain: form.domain.trim(),
    };
    const nextErrors: FormErrors = {};

    if (dialogMode === "create") {
      nextErrors.name = validateIdentifier(nextForm.name, "Database name");
    }
    if (dialogMode === "create") {
      nextErrors.username = validateIdentifier(nextForm.username, "Username");
    } else {
      nextErrors.current_username = validateIdentifier(nextForm.currentUsername, "Current username");
      nextErrors.username = validateIdentifier(nextForm.username, "Username");
    }

    if (dialogMode === "create" && nextForm.password.length < 8) {
      nextErrors.password = "Password must be at least 8 characters.";
    }
    if (dialogMode === "edit" && nextForm.password.length > 0 && nextForm.password.length < 8) {
      nextErrors.password = "Password must be at least 8 characters.";
    }
    if (
      dialogMode === "edit" &&
      nextForm.password.length === 0 &&
      nextForm.currentUsername !== nextForm.username
    ) {
      nextErrors.password = "Password is required when changing username.";
    }

    setErrors(nextErrors);
    if (nextErrors.name || nextErrors.current_username || nextErrors.username || nextErrors.password) {
      return;
    }

    setSubmitting(true);
    setFormError(null);

    try {
      if (dialogMode === "create") {
        const payload: CreateMariaDBDatabaseInput = {
          name: nextForm.name,
          username: nextForm.username,
          password: nextForm.password,
          domain: nextForm.domain || undefined,
        };
        await createMariaDBDatabase(payload);
      } else {
        await updateMariaDBDatabase(nextForm.name, {
          current_username: nextForm.currentUsername,
          username: nextForm.username,
          password: nextForm.password,
          domain: nextForm.domain || undefined,
        });
      }

      await reloadDatabases();
      setDialogMode(null);
      resetForm();
    } catch (error) {
      const mariadbError = error as MariaDBApiError;
      if (mariadbError.fieldErrors) {
        setErrors({
          name: mariadbError.fieldErrors.name,
          current_username: mariadbError.fieldErrors.current_username,
          username: mariadbError.fieldErrors.username,
          password: mariadbError.fieldErrors.password,
        });
      }
      setFormError(
        getErrorMessage(
          error,
          dialogMode === "create" ? "Failed to create database." : "Failed to update database.",
        ),
      );
    } finally {
      setSubmitting(false);
    }
  }

  async function handleDelete(database: MariaDBDatabase) {
    if (submitting || deletingName !== null) {
      return;
    }

    const confirmed = window.confirm(
      `Delete ${database.name}? This will remove the database and may remove user ${database.username} if unused.`,
    );
    if (!confirmed) {
      return;
    }

    setDeletingName(database.name);
    try {
      await deleteMariaDBDatabase(database.name, database.username);
      await reloadDatabases();
    } catch (error) {
      setLoadError(getErrorMessage(error, `Failed to delete ${database.name}.`));
    } finally {
      setDeletingName(null);
    }
  }

  function handleTogglePasswordVisibility(database: MariaDBDatabase) {
    const key = getDatabasePasswordKey(database);
    setVisiblePasswords((current) => ({
      ...current,
      [key]: !current[key],
    }));
  }

  async function handleCopyPassword(database: MariaDBDatabase) {
    if (!database.password) {
      return;
    }

    const key = getDatabasePasswordKey(database);

    try {
      await navigator.clipboard.writeText(database.password);
      setCopiedPasswordKey(key);
      window.setTimeout(() => {
        setCopiedPasswordKey((current) => (current === key ? null : current));
      }, 1500);
      toast.success(`Password copied for ${database.name}.`);
    } catch {
      toast.error(`Failed to copy password for ${database.name}.`);
    }
  }

  return (
    <>
      <div className="px-4 py-6 sm:px-6 lg:px-8">
        <section className="space-y-4">
          {loadError ? (
            <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
              {loadError}
            </section>
          ) : null}

          <section className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-surface)]">
            <div className="flex flex-wrap items-center gap-2 border-b border-[var(--app-border)] px-3 py-3">
              <Button
                type="button"
                onClick={openCreateDialog}
                className="h-10 rounded-lg border border-emerald-700/50 bg-emerald-600 px-4 text-[13px] font-medium text-white hover:bg-emerald-500"
              >
                <Plus className="h-4 w-4" />
                Add DB
              </Button>

              <Popover open={rootPasswordOpen} onOpenChange={setRootPasswordOpen}>
                <PopoverTrigger asChild>
                  <Button
                    type="button"
                    variant="ghost"
                    className="h-10 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 text-[13px] font-medium text-[var(--app-text)]"
                  >
                    Root password
                  </Button>
                </PopoverTrigger>
                <PopoverContent
                  align="start"
                  sideOffset={8}
                  className="w-[320px] rounded-lg border-[var(--app-border)] bg-[var(--app-surface)] p-3"
                >
                  <div className="space-y-3">
                    <div className="text-[12px] font-medium text-[var(--app-text-muted)]">
                      MariaDB root password
                    </div>
                    {rootPasswordLoading ? (
                      <div className="text-[13px] text-[var(--app-text-muted)]">Loading...</div>
                    ) : (
                      <div className="space-y-2">
                        {!rootPasswordConfigured ? (
                          <div className="text-[13px] text-[var(--app-text-muted)]">
                            No root password configured.
                          </div>
                        ) : null}

                        <div className="relative">
                          <Input
                            type="text"
                            value={rootPasswordDraft}
                            onChange={(event) => setRootPasswordDraft(event.target.value)}
                            placeholder="Set MariaDB root password"
                            autoComplete="off"
                            className="h-9 rounded-md border-[var(--app-border)] bg-[var(--app-surface-muted)] pr-10 font-mono text-[13px]"
                          />
                          <button
                            type="button"
                            onClick={handleGenerateRootPassword}
                            className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded-md p-1 text-[var(--app-text-muted)] hover:bg-[var(--app-surface)] hover:text-[var(--app-text)]"
                            aria-label="Generate a new password"
                            title="Generate password"
                          >
                            <RefreshCw className="h-4 w-4" />
                          </button>
                        </div>

                        {rootPasswordTooShort ? (
                          <div className="text-[12px] text-[var(--app-danger)]">
                            Password must be at least 8 characters.
                          </div>
                        ) : null}
                        {rootPasswordError ? (
                          <div className="text-[12px] text-[var(--app-danger)]">{rootPasswordError}</div>
                        ) : null}

                        <div className="flex items-center justify-end gap-2 pt-1">
                          <Button type="button" variant="secondary" onClick={handleCancelRootPasswordEdit}>
                            Cancel
                          </Button>
                          <Button
                            type="button"
                            onClick={() => {
                              void handleSaveRootPassword();
                            }}
                            disabled={
                              rootPasswordSaving ||
                              rootPasswordLoading ||
                              !rootPasswordDirty ||
                              rootPasswordCandidate.length < 8
                            }
                          >
                            {rootPasswordSaving ? "Saving..." : "Save"}
                          </Button>
                        </div>
                      </div>
                    )}
                  </div>
                </PopoverContent>
              </Popover>
              <ToolbarButton>phpMyAdmin</ToolbarButton>

              <small>{statusSummary}</small>

              <div className="ms-auto flex items-center gap-2">
                <label className="relative block min-w-[220px]">
                  <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--app-text-muted)]" />
                  <Input
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    placeholder="Database search"
                    className="h-10 rounded-lg border-[var(--app-border)] bg-[var(--app-surface-muted)] pl-9"
                  />
                </label>
              </div>
            </div>

            <div className="overflow-x-auto">
              <table className="min-w-[1040px] w-full text-left">
                <thead className="border-b border-[var(--app-border)] bg-[var(--app-surface)]">
                  <tr className="text-[13px] text-[var(--app-text-muted)]">
                    <th className="w-[46px] px-3 py-3">
                      <input type="checkbox" aria-label="Select all" className="h-4 w-4 rounded border-[var(--app-border)]" />
                    </th>
                    <th className="px-3 py-3 font-medium">Database name</th>
                    <th className="px-3 py-3 font-medium">Username</th>
                    <th className="px-3 py-3 font-medium">Password</th>
                    <th className="px-3 py-3 font-medium">Backup</th>
                    <th className="px-3 py-3 font-medium">Location</th>
                    <th className="px-3 py-3 font-medium">Domain</th>
                    <th className="px-3 py-3 text-right font-medium">Operate</th>
                  </tr>
                </thead>
                <tbody>
                  {loading ? (
                    <tr>
                      <td colSpan={8} className="px-3 py-8 text-center text-[13px] text-[var(--app-text-muted)]">
                        Loading databases...
                      </td>
                    </tr>
                  ) : filteredDatabases.length === 0 ? (
                    <tr>
                      <td colSpan={8} className="px-3 py-8 text-center text-[13px] text-[var(--app-text-muted)]">
                        No databases found.
                      </td>
                    </tr>
                  ) : (
                    filteredDatabases.map((database) => (
                      <tr
                        key={database.name}
                        className="border-b border-[var(--app-border)] text-[14px] text-[var(--app-text)] last:border-b-0"
                      >
                        <td className="px-3 py-3 align-middle">
                          <input type="checkbox" aria-label={`Select ${database.name}`} className="h-4 w-4 rounded border-[var(--app-border)]" />
                        </td>
                        <td className="px-3 py-3 align-middle">{database.name}</td>
                        <td className="px-3 py-3 align-middle">{database.username || "Not set"}</td>
                        <td className="px-3 py-3 align-middle">
                          {database.password ? (
                            <div className="flex items-center gap-1.5 whitespace-nowrap">
                              <span className="font-mono text-[13px] text-[var(--app-text-muted)]">
                                {visiblePasswords[getDatabasePasswordKey(database)]
                                  ? database.password
                                  : maskPassword(database.password)}
                              </span>
                              <button
                                type="button"
                                onClick={() => handleTogglePasswordVisibility(database)}
                                className="rounded-md p-1 text-[var(--app-text-muted)] transition hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)]"
                                aria-label={
                                  visiblePasswords[getDatabasePasswordKey(database)] ? "Hide password" : "Show password"
                                }
                                title={
                                  visiblePasswords[getDatabasePasswordKey(database)] ? "Hide password" : "Show password"
                                }
                              >
                                {visiblePasswords[getDatabasePasswordKey(database)] ? (
                                  <EyeOff className="h-4 w-4" />
                                ) : (
                                  <Eye className="h-4 w-4" />
                                )}
                              </button>
                              <button
                                type="button"
                                onClick={() => {
                                  void handleCopyPassword(database);
                                }}
                                className="rounded-md p-1 text-[var(--app-text-muted)] transition hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)]"
                                aria-label={`Copy password for ${database.name}`}
                                title={
                                  copiedPasswordKey === getDatabasePasswordKey(database)
                                    ? "Copied"
                                    : "Copy password"
                                }
                              >
                                {copiedPasswordKey === getDatabasePasswordKey(database) ? (
                                  <Check className="h-4 w-4 text-emerald-500" />
                                ) : (
                                  <Copy className="h-4 w-4" />
                                )}
                              </button>
                            </div>
                          ) : null}
                        </td>
                        <td className="px-3 py-3 align-middle text-[var(--app-text-muted)]">Not set</td>
                        <td className="px-3 py-3 align-middle">{database.host || "localhost"}</td>
                        <td className="px-3 py-3 align-middle text-[var(--app-text-muted)]">{database.domain || ""}</td>
                        <td className="px-3 py-3 align-middle text-right">
                          <div className="flex items-center justify-end gap-2 text-[13px]">
                            <button
                              type="button"
                              onClick={() => openEditDialog(database)}
                              className="inline-flex items-center gap-1 text-emerald-500 hover:text-emerald-400"
                            >
                              <Pencil className="h-3.5 w-3.5" />
                              Edit
                            </button>
                            <span className="text-[var(--app-border)]">|</span>
                            <button
                              type="button"
                              onClick={() => {
                                void handleDelete(database);
                              }}
                              disabled={deletingName !== null}
                              className={cn(
                                "inline-flex items-center gap-1 text-red-400 hover:text-red-300",
                                deletingName !== null ? "opacity-60" : "",
                              )}
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                              {deletingName === database.name ? "Deleting..." : "Delete"}
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>

            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-[var(--app-border)] px-3 py-3">
              <div className="flex items-center gap-2">
                <select className="h-10 min-w-[140px] rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 text-[13px] text-[var(--app-text-muted)]">
                  <option>Please choose</option>
                </select>
                <Button
                  type="button"
                  variant="ghost"
                  disabled
                  className="h-10 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 text-[13px] text-[var(--app-text-muted)]"
                >
                  Execute
                </Button>
              </div>

              <div className="flex items-center gap-2 text-[13px] text-[var(--app-text-muted)]">
                <Button type="button" variant="ghost" disabled className="h-8 w-8 rounded-lg border border-[var(--app-border)] p-0">
                  {"<"}
                </Button>
                <span className="inline-flex h-8 min-w-8 items-center justify-center rounded-lg border border-[var(--app-border)] px-2">
                  1
                </span>
                <Button type="button" variant="ghost" disabled className="h-8 w-8 rounded-lg border border-[var(--app-border)] p-0">
                  {">"}
                </Button>
                <select className="h-8 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-2 text-[13px]">
                  <option>20 / page</option>
                </select>
                <span>Goto</span>
                <Input value="1" readOnly className="h-8 w-16 rounded-lg border-[var(--app-border)] bg-[var(--app-surface-muted)] px-2 text-center text-[13px]" />
                <span>Total {filteredDatabases.length}</span>
              </div>
            </div>
          </section>
        </section>
      </div>

      <Dialog
        open={dialogMode !== null}
        onOpenChange={(open) => {
          if (!open) {
            closeDialog();
          }
        }}
      >
        <DialogContent
          className="sm:max-w-xl"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            nameInputRef.current?.focus();
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
            <DialogTitle>{formTitle}</DialogTitle>
            <DialogDescription>{formDescription}</DialogDescription>
          </DialogHeader>

          {formError ? (
            <section className="rounded-[10px] border border-[var(--app-danger)]/40 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
              {formError}
            </section>
          ) : null}

          <form onSubmit={handleSubmit} className="space-y-5">
            <div className="space-y-2">
              <label htmlFor="database-name" className="text-[13px] font-medium text-[var(--app-text)]">
                Database name
              </label>
              <Input
                id="database-name"
                ref={nameInputRef}
                value={form.name}
                readOnly={dialogMode !== "create"}
                onChange={(event) => {
                  setForm((current) => ({ ...current, name: event.target.value }));
                  if (errors.name) {
                    setErrors((current) => ({ ...current, name: undefined }));
                  }
                }}
                placeholder="project_db"
                autoComplete="off"
                className={cn(
                  dialogMode !== "create" ? "bg-[var(--app-surface-muted)]" : "",
                  errors.name ? "border-[var(--app-danger)]" : "",
                )}
              />
              {errors.name ? <p className="text-[12px] text-[var(--app-danger)]">{errors.name}</p> : null}
            </div>

            <div className="space-y-2">
              <label htmlFor="database-username" className="text-[13px] font-medium text-[var(--app-text)]">
                Username
              </label>
              <Input
                id="database-username"
                value={form.username}
                onChange={(event) => {
                  setForm((current) => ({ ...current, username: event.target.value }));
                  if (errors.username) {
                    setErrors((current) => ({ ...current, username: undefined }));
                  }
                }}
                placeholder="project_user"
                autoComplete="off"
                className={errors.username ? "border-[var(--app-danger)]" : ""}
              />
              {errors.username ? <p className="text-[12px] text-[var(--app-danger)]">{errors.username}</p> : null}
            </div>

            <div className="space-y-2">
              <label htmlFor="database-password" className="text-[13px] font-medium text-[var(--app-text)]">
                Password
              </label>
              <div className="relative">
                <Input
                  id="database-password"
                  type="text"
                  value={form.password}
                  onChange={(event) => {
                    setForm((current) => ({ ...current, password: event.target.value }));
                    if (errors.password) {
                      setErrors((current) => ({ ...current, password: undefined }));
                    }
                  }}
                  placeholder={dialogMode === "create" ? "At least 8 characters" : "Leave blank to keep current password"}
                  autoComplete="new-password"
                  className={cn("pr-10", errors.password ? "border-[var(--app-danger)]" : "")}
                />
                <button
                  type="button"
                  onClick={handleGenerateDatabasePassword}
                  className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded-md p-1 text-[var(--app-text-muted)] hover:bg-[var(--app-surface)] hover:text-[var(--app-text)]"
                  aria-label="Generate a new password"
                  title="Generate password"
                >
                  <RefreshCw className="h-4 w-4" />
                </button>
              </div>
              {errors.password ? (
                <p className="text-[12px] text-[var(--app-danger)]">{errors.password}</p>
              ) : null}
            </div>

            <div className="space-y-2">
              <label htmlFor="database-domain" className="text-[13px] font-medium text-[var(--app-text)]">
                Domain
              </label>
              <select
                id="database-domain"
                value={form.domain}
                onChange={(event) => {
                  setForm((current) => ({ ...current, domain: event.target.value }));
                  if (errors.domain) {
                    setErrors((current) => ({ ...current, domain: undefined }));
                  }
                }}
                className={cn(
                  "h-10 w-full rounded-md border border-[var(--app-border)] bg-[var(--app-surface)] px-3 text-[14px] text-[var(--app-text)] focus:outline-none",
                  errors.domain ? "border-[var(--app-danger)]" : "",
                )}
              >
                <option value="">No domain</option>
                {selectedDomainMissing ? <option value={form.domain}>{form.domain}</option> : null}
                {domains.map((domain) => (
                  <option key={domain.id} value={domain.hostname}>
                    {domain.hostname}
                  </option>
                ))}
              </select>
              {domainsLoadError ? (
                <p className="text-[12px] text-[var(--app-text-muted)]">{domainsLoadError}</p>
              ) : null}
              {errors.domain ? <p className="text-[12px] text-[var(--app-danger)]">{errors.domain}</p> : null}
            </div>

            <DialogFooter className="border-t border-[var(--app-border)] pt-4">
              <Button type="button" variant="secondary" onClick={closeDialog} disabled={submitting}>
                Cancel
              </Button>
              <Button type="submit" disabled={submitting}>
                {submitting
                  ? dialogMode === "create"
                    ? "Creating..."
                    : "Saving..."
                  : dialogMode === "create"
                    ? "Create database"
                    : "Save changes"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
