import {
  useDeferredValue,
  useEffect,
  useMemo,
  useState,
  type FormEvent,
} from "react";
import {
  createFTPAccount,
  deleteFTPAccount,
  fetchFTPAccounts,
  updateFTPAccount,
  type CreateFTPAccountInput,
  type FTPAccount,
  type FTPApiError,
} from "@/api/ftp";
import { fetchDomains, type DomainRecord } from "@/api/domains";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { LongText } from "@/components/long-text";
import { PasswordInput } from "@/components/password-input";
import { Pencil, Plus, Search, Trash2 } from "@/components/icons/tabler-icons";
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
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { formatDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

type DialogMode = "create" | "edit" | null;

type FormState = {
  username: string;
  password: string;
  rootPath: string;
  domainID: string;
  enabled: boolean;
};

type FormErrors = {
  username?: string;
  password?: string;
  root_path?: string;
  domain_id?: string;
  enabled?: string;
};

const initialForm: FormState = {
  username: "",
  password: "",
  rootPath: "",
  domainID: "",
  enabled: true,
};

const supportedDomainKinds = new Set([
  "Static site",
  "Php site",
  "Node.js",
  "Python",
]);
const tableHeaderCellClass = "px-3 py-2 text-left text-[13px] font-medium text-[var(--app-text-muted)]";
const tableBodyCellClass = "px-3 py-3 align-middle text-[14px] text-[var(--app-text)]";
const actionButtonClass =
  "inline-flex h-9 w-9 items-center justify-center rounded-lg text-[var(--app-text-muted)] transition hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)] disabled:cursor-not-allowed disabled:opacity-60";
const dangerActionButtonClass =
  "inline-flex h-9 w-9 items-center justify-center rounded-lg text-[var(--app-danger)] transition hover:bg-[var(--app-danger-soft)] hover:text-[var(--app-danger)] disabled:cursor-not-allowed disabled:opacity-60";

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

function generateFTPPassword(length = 20) {
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
  const randomBytes = new Uint8Array(length);

  if (globalThis.crypto?.getRandomValues) {
    globalThis.crypto.getRandomValues(randomBytes);
  } else {
    for (let index = 0; index < randomBytes.length; index += 1) {
      randomBytes[index] = Math.floor(Math.random() * 256);
    }
  }

  return Array.from(randomBytes, (value) => alphabet[value % alphabet.length]).join("");
}

function normalizeRelativeRootPath(value: string, sitesBasePath: string) {
  const trimmedValue = value.trim();
  if (!trimmedValue) {
    return "";
  }
  if (!sitesBasePath) {
    return trimmedValue;
  }

  const normalizedBasePath = sitesBasePath.replace(/[\\/]+$/, "");
  if (trimmedValue.startsWith(normalizedBasePath)) {
    return trimmedValue;
  }

  const normalizedValue = trimmedValue.replace(/^[/\\]+/, "");
  return `${normalizedBasePath}/${normalizedValue}`;
}

export function FTPPage() {
  const [accounts, setAccounts] = useState<FTPAccount[]>([]);
  const [domains, setDomains] = useState<DomainRecord[]>([]);
  const [sitesBasePath, setSitesBasePath] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [domainsLoadError, setDomainsLoadError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [dialogMode, setDialogMode] = useState<DialogMode>(null);
  const [editingAccount, setEditingAccount] = useState<FTPAccount | null>(null);
  const [form, setForm] = useState<FormState>(initialForm);
  const [errors, setErrors] = useState<FormErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [deleteCandidate, setDeleteCandidate] = useState<FTPAccount | null>(null);
  const [deletingID, setDeletingID] = useState<string | null>(null);
  const deferredSearch = useDeferredValue(search);

  const eligibleDomains = useMemo(
    () => domains.filter((domain) => supportedDomainKinds.has(domain.kind)),
    [domains],
  );

  useEffect(() => {
    let active = true;

    async function loadData() {
      try {
        const [accountsResult, domainsResult] = await Promise.allSettled([
          fetchFTPAccounts(),
          fetchDomains(),
        ]);

        if (!active) {
          return;
        }

        if (accountsResult.status === "fulfilled") {
          setAccounts(accountsResult.value.accounts);
          setLoadError(null);
        } else {
          setAccounts([]);
          setLoadError(getErrorMessage(accountsResult.reason, "Failed to load FTP accounts."));
        }

        if (domainsResult.status === "fulfilled") {
          setDomains(domainsResult.value.domains);
          setSitesBasePath(domainsResult.value.sites_base_path);
          setDomainsLoadError(null);
        } else {
          setDomains([]);
          setSitesBasePath("");
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

  const normalizedSearch = deferredSearch.trim().toLowerCase();
  const filteredAccounts = accounts.filter((account) => {
    if (!normalizedSearch) {
      return true;
    }

    return [
      account.username,
      account.root_path,
      account.domain_name ?? "",
      account.enabled ? "enabled" : "disabled",
      account.has_password ? "configured" : "missing",
    ]
      .join(" ")
      .toLowerCase()
      .includes(normalizedSearch);
  });

  const formTitle = dialogMode === "create" ? "Create FTP account" : "Edit FTP account";
  const formDescription =
    dialogMode === "create"
      ? "Create an FTP user with a document root limited to the panel sites directory."
      : "Update the FTP account. Leave password empty to keep the current password.";

  function resetForm() {
    setForm(initialForm);
    setErrors({});
    setFormError(null);
    setSubmitting(false);
  }

  function openCreateDialog() {
    setEditingAccount(null);
    resetForm();
    setForm({
      ...initialForm,
      rootPath: sitesBasePath,
    });
    setDialogMode("create");
  }

  function openEditDialog(account: FTPAccount) {
    setEditingAccount(account);
    setErrors({});
    setFormError(null);
    setSubmitting(false);
    setForm({
      username: account.username,
      password: "",
      rootPath: account.root_path,
      domainID: account.domain_id,
      enabled: account.enabled,
    });
    setDialogMode("edit");
  }

  function closeDialog() {
    if (submitting) {
      return;
    }

    setDialogMode(null);
    setEditingAccount(null);
    resetForm();
  }

  async function reloadAccounts() {
    const payload = await fetchFTPAccounts();
    setAccounts(payload.accounts);
    setLoadError(null);
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!dialogMode) {
      return;
    }

    const nextForm: FormState = {
      username: form.username.trim().toLowerCase(),
      password: form.password.trim(),
      rootPath: normalizeRelativeRootPath(form.rootPath, sitesBasePath),
      domainID: form.domainID.trim(),
      enabled: form.enabled,
    };

    const nextErrors: FormErrors = {};
    if (!nextForm.username) {
      nextErrors.username = "FTP username is required.";
    }
    if (dialogMode === "create" && !nextForm.password) {
      nextErrors.password = "FTP password is required.";
    }
    if (!nextForm.rootPath) {
      nextErrors.root_path = "Document root is required.";
    }

    setErrors(nextErrors);
    if (nextErrors.username || nextErrors.password || nextErrors.root_path) {
      return;
    }

    setSubmitting(true);
    setFormError(null);

    try {
      if (dialogMode === "create") {
        const payload: CreateFTPAccountInput = {
          username: nextForm.username,
          password: nextForm.password,
          root_path: nextForm.rootPath,
          domain_id: nextForm.domainID || undefined,
          enabled: nextForm.enabled,
        };
        await createFTPAccount(payload);
        toast.success(`Created FTP account ${nextForm.username}.`);
      } else if (editingAccount) {
        await updateFTPAccount(editingAccount.id, {
          username: nextForm.username,
          password: nextForm.password || undefined,
          root_path: nextForm.rootPath,
          domain_id: nextForm.domainID || undefined,
          enabled: nextForm.enabled,
        });
        toast.success(`Updated FTP account ${nextForm.username}.`);
      }

      await reloadAccounts();
      closeDialog();
    } catch (error) {
      const ftpError = error as FTPApiError;
      if (ftpError.fieldErrors) {
        setErrors({
          username: ftpError.fieldErrors.username,
          password: ftpError.fieldErrors.password,
          root_path: ftpError.fieldErrors.root_path,
          domain_id: ftpError.fieldErrors.domain_id,
          enabled: ftpError.fieldErrors.enabled,
        });
      }
      setFormError(
        getErrorMessage(
          error,
          dialogMode === "create" ? "Failed to create FTP account." : "Failed to update FTP account.",
        ),
      );
    } finally {
      setSubmitting(false);
    }
  }

  async function handleConfirmDelete() {
    if (!deleteCandidate) {
      return;
    }

    const account = deleteCandidate;
    setDeletingID(account.id);

    try {
      await deleteFTPAccount(account.id);
      await reloadAccounts();
      toast.success(`Deleted FTP account ${account.username}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, `Failed to delete ${account.username}.`));
    } finally {
      setDeletingID(null);
      setDeleteCandidate((current) => (current?.id === account.id ? null : current));
    }
  }

  return (
    <>
      <ActionConfirmDialog
        open={deleteCandidate !== null}
        onOpenChange={(open) => {
          if (!open && deletingID === null) {
            setDeleteCandidate(null);
          }
        }}
        title="Delete FTP account"
        desc={
          deleteCandidate
            ? `Delete FTP account ${deleteCandidate.username}? This removes access immediately.`
            : "Delete this FTP account?"
        }
        confirmText="Delete account"
        destructive
        isLoading={deleteCandidate !== null && deletingID === deleteCandidate.id}
        handleConfirm={() => {
          void handleConfirmDelete();
        }}
        className="sm:max-w-md"
      />

      <Dialog open={dialogMode !== null} onOpenChange={(open) => (!open ? closeDialog() : undefined)}>
        <DialogContent className="sm:max-w-2xl">
          <form onSubmit={handleSubmit} className="space-y-5">
            <DialogHeader>
              <DialogTitle>{formTitle}</DialogTitle>
              <DialogDescription>{formDescription}</DialogDescription>
            </DialogHeader>

            {formError ? (
              <section className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
                {formError}
              </section>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="ftp-username">FTP username</Label>
                <Input
                  id="ftp-username"
                  autoFocus
                  value={form.username}
                  onChange={(event) => {
                    setForm((current) => ({ ...current, username: event.target.value }));
                    if (errors.username) {
                      setErrors((current) => ({ ...current, username: undefined }));
                    }
                  }}
                  autoComplete="off"
                  aria-invalid={errors.username ? "true" : "false"}
                  className={errors.username ? "border-[var(--app-danger)]" : ""}
                />
                {errors.username ? (
                  <p className="text-[12px] text-[var(--app-danger)]">{errors.username}</p>
                ) : null}
              </div>

              <div className="space-y-2">
                <Label htmlFor="ftp-domain">Domain</Label>
                <Select
                  value={form.domainID || "__none__"}
                  onValueChange={(value) => {
                    const nextDomainID = value === "__none__" ? "" : value;
                    const selectedDomain = eligibleDomains.find((domain) => domain.id === nextDomainID);
                    setForm((current) => ({
                      ...current,
                      domainID: nextDomainID,
                      rootPath:
                        current.rootPath.trim() === "" && selectedDomain
                          ? selectedDomain.target
                          : current.rootPath,
                    }));
                    if (errors.domain_id) {
                      setErrors((current) => ({ ...current, domain_id: undefined }));
                    }
                  }}
                >
                  <SelectTrigger
                    id="ftp-domain"
                    className={cn("w-full", errors.domain_id ? "border-[var(--app-danger)]" : "")}
                    aria-invalid={errors.domain_id ? "true" : "false"}
                  >
                    <SelectValue placeholder="No linked domain" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__none__">No linked domain</SelectItem>
                    {eligibleDomains.map((domain) => (
                      <SelectItem key={domain.id} value={domain.id}>
                        {domain.hostname}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {errors.domain_id ? (
                  <p className="text-[12px] text-[var(--app-danger)]">{errors.domain_id}</p>
                ) : null}
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="ftp-password">Password</Label>
              <PasswordInput
                id="ftp-password"
                value={form.password}
                onChange={(event) => {
                  setForm((current) => ({ ...current, password: event.target.value }));
                  if (errors.password) {
                    setErrors((current) => ({ ...current, password: undefined }));
                  }
                }}
                onGeneratePassword={() => {
                  setForm((current) => ({ ...current, password: generateFTPPassword() }));
                  if (errors.password) {
                    setErrors((current) => ({ ...current, password: undefined }));
                  }
                }}
                placeholder={dialogMode === "edit" ? "Leave empty to keep current password" : "Set FTP password"}
                autoComplete="new-password"
                aria-invalid={errors.password ? "true" : "false"}
                className={errors.password ? "border-[var(--app-danger)]" : ""}
              />
              {errors.password ? (
                <p className="text-[12px] text-[var(--app-danger)]">{errors.password}</p>
              ) : dialogMode === "edit" ? (
                <p className="text-[12px] text-[var(--app-text-muted)]">
                  Password stays unchanged until you enter a new one.
                </p>
              ) : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="ftp-root-path">Document root</Label>
              <Input
                id="ftp-root-path"
                value={form.rootPath}
                onChange={(event) => {
                  setForm((current) => ({ ...current, rootPath: event.target.value }));
                  if (errors.root_path) {
                    setErrors((current) => ({ ...current, root_path: undefined }));
                  }
                }}
                placeholder={sitesBasePath ? `${sitesBasePath}/example.com/public` : "example.com/public"}
                autoComplete="off"
                aria-invalid={errors.root_path ? "true" : "false"}
                className={errors.root_path ? "border-[var(--app-danger)]" : ""}
              />
              {errors.root_path ? (
                <p className="text-[12px] text-[var(--app-danger)]">{errors.root_path}</p>
              ) : (
                <p className="text-[12px] text-[var(--app-text-muted)]">
                  Must stay inside {sitesBasePath || "the panel sites directory"}. Relative paths resolve there automatically.
                </p>
              )}
            </div>

            <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-4">
              <div className="space-y-1">
                <Label htmlFor="ftp-account-enabled">FTP account enabled</Label>
                <p className="text-[12px] text-[var(--app-text-muted)]">
                  Disabled accounts keep their configuration but cannot log in.
                </p>
                {errors.enabled ? (
                  <p className="text-[12px] text-[var(--app-danger)]">{errors.enabled}</p>
                ) : null}
              </div>
              <Switch
                id="ftp-account-enabled"
                checked={form.enabled}
                disabled={submitting}
                onCheckedChange={(checked) => {
                  setForm((current) => ({ ...current, enabled: checked }));
                  if (errors.enabled) {
                    setErrors((current) => ({ ...current, enabled: undefined }));
                  }
                }}
              />
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={closeDialog} disabled={submitting}>
                Cancel
              </Button>
              <Button type="submit" disabled={submitting}>
                {submitting
                  ? dialogMode === "create"
                    ? "Creating..."
                    : "Saving..."
                  : dialogMode === "create"
                    ? "Create account"
                    : "Save changes"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <div className="px-4 py-6 sm:px-6 lg:px-8">
        <section className="space-y-4">
          {loadError ? (
            <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
              {loadError}
            </section>
          ) : null}

          <section className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
            <div className="flex flex-wrap items-center gap-2 border-b border-[var(--app-border)] px-3 py-3">
              <Button
                type="button"
                onClick={openCreateDialog}
                className="h-10 rounded-lg border border-emerald-700/50 bg-emerald-600 px-4 text-[13px] font-medium text-white hover:bg-emerald-500"
              >
                <Plus className="h-4 w-4" />
                Add FTP
              </Button>

              <div className="ms-auto flex items-center gap-2">
                <label className="relative block min-w-[240px]">
                  <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--app-text-muted)]" />
                  <Input
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    placeholder="Search FTP accounts"
                    className="h-10 rounded-lg border-[var(--app-border)] bg-[var(--app-surface-muted)] pl-9"
                  />
                </label>
              </div>
            </div>

            {domainsLoadError ? (
              <div className="border-b border-[var(--app-border)] bg-[var(--app-surface)] px-3 py-2 text-[12px] text-[var(--app-text-muted)]">
                {domainsLoadError}
              </div>
            ) : null}

            <div className="overflow-x-auto">
              <table className="min-w-[980px] w-full">
                <thead className="border-b border-[var(--app-border)] bg-[var(--app-surface)]">
                  <tr>
                    <th className={tableHeaderCellClass}>FTP user</th>
                    <th className={tableHeaderCellClass}>Password</th>
                    <th className={tableHeaderCellClass}>Status</th>
                    <th className={tableHeaderCellClass}>Document root</th>
                    <th className={tableHeaderCellClass}>Domain</th>
                    <th className={tableHeaderCellClass}>Updated</th>
                    <th className={`${tableHeaderCellClass} text-right`}>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {loading ? (
                    <tr>
                      <td colSpan={7} className="px-3 py-10 text-center text-[13px] text-[var(--app-text-muted)]">
                        Loading FTP accounts...
                      </td>
                    </tr>
                  ) : filteredAccounts.length === 0 ? (
                    <tr>
                      <td colSpan={7} className="px-3 py-10 text-center text-[13px] text-[var(--app-text-muted)]">
                        No FTP accounts found.
                      </td>
                    </tr>
                  ) : (
                    filteredAccounts.map((account) => (
                      <tr
                        key={account.id}
                        className="border-b border-[var(--app-border)] last:border-b-0"
                      >
                        <td className={tableBodyCellClass}>
                          <div className="font-medium">{account.username}</div>
                        </td>
                        <td className={tableBodyCellClass}>
                          <span
                            className={cn(
                              "text-[13px]",
                              account.has_password
                                ? "text-[var(--app-text)]"
                                : "text-[var(--app-text-muted)]",
                            )}
                          >
                            {account.has_password ? "Configured" : "Missing"}
                          </span>
                        </td>
                        <td className={tableBodyCellClass}>
                          <span
                            className={cn(
                              "text-[13px] font-medium",
                              account.enabled
                                ? "text-emerald-600"
                                : "text-[var(--app-text-muted)]",
                            )}
                          >
                            {account.enabled ? "Enabled" : "Disabled"}
                          </span>
                        </td>
                        <td className={tableBodyCellClass}>
                          <LongText className="max-w-[320px] font-mono text-[13px]">
                            {account.root_path}
                          </LongText>
                        </td>
                        <td className={tableBodyCellClass}>
                          {account.domain_name ? account.domain_name : "Not linked"}
                        </td>
                        <td className={tableBodyCellClass}>{formatDateTime(account.updated_at)}</td>
                        <td className={`${tableBodyCellClass} text-right`}>
                          <div className="flex items-center justify-end gap-1">
                            <button
                              type="button"
                              onClick={() => openEditDialog(account)}
                              className={actionButtonClass}
                              aria-label={`Edit ${account.username}`}
                              title="Edit FTP account"
                            >
                              <Pencil className="h-4 w-4" />
                            </button>
                            <button
                              type="button"
                              onClick={() => setDeleteCandidate(account)}
                              className={dangerActionButtonClass}
                              aria-label={`Delete ${account.username}`}
                              title="Delete FTP account"
                            >
                              <Trash2 className="h-4 w-4" />
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </section>
        </section>
      </div>
    </>
  );
}
