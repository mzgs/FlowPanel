import { useEffect, useState } from "react";
import { fetchFTPAccounts, type FTPAccount } from "@/api/ftp";
import {
  fetchDomainFTPStatus,
  updateDomainFTP,
  type DomainApiError,
  type DomainFTPStatus,
  type DomainRecord,
} from "@/api/domains";
import { LoaderCircle } from "@/components/icons/tabler-icons";
import { PasswordInput } from "@/components/password-input";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

type FTPFormState = {
  username: string;
  password: string;
  enabled: boolean;
};

type FTPFormErrors = {
  username?: string;
  password?: string;
  enabled?: string;
};

const initialFTPFormState: FTPFormState = {
  username: "",
  password: "",
  enabled: false,
};

const ftpGeneratedPasswordLength = 20;

function getDomainAccounts(
  accounts: FTPAccount[],
  domainID: string,
  status: DomainFTPStatus | null,
) {
  return accounts
    .filter((account) => account.domain_id === domainID)
    .sort((left, right) => {
      const leftManaged =
        status !== null &&
        left.username === status.username &&
        left.root_path === status.root_path;
      const rightManaged =
        status !== null &&
        right.username === status.username &&
        right.root_path === status.root_path;

      if (leftManaged !== rightManaged) {
        return leftManaged ? -1 : 1;
      }

      return left.username.localeCompare(right.username);
    });
}

function generateFTPPassword(length = ftpGeneratedPasswordLength) {
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

type DomainFTPDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  domain: DomainRecord | null;
};

export function DomainFTPDialog({
  open,
  onOpenChange,
  domain,
}: DomainFTPDialogProps) {
  const [ftpStatus, setFTPStatus] = useState<DomainFTPStatus | null>(null);
  const [domainAccounts, setDomainAccounts] = useState<FTPAccount[]>([]);
  const [ftpForm, setFTPForm] = useState<FTPFormState>(initialFTPFormState);
  const [ftpErrors, setFTPErrors] = useState<FTPFormErrors>({});
  const [ftpLoadError, setFTPLoadError] = useState<string | null>(null);
  const [accountsLoadError, setAccountsLoadError] = useState<string | null>(null);
  const [ftpLoading, setFTPLoading] = useState(false);
  const [accountsLoading, setAccountsLoading] = useState(false);
  const [ftpSaving, setFTPSaving] = useState(false);

  useEffect(() => {
    if (!open || !domain) {
      setFTPStatus(null);
      setDomainAccounts([]);
      setFTPForm(initialFTPFormState);
      setFTPErrors({});
      setFTPLoadError(null);
      setAccountsLoadError(null);
      setFTPLoading(false);
      setAccountsLoading(false);
      setFTPSaving(false);
      return;
    }

    let active = true;
    setFTPStatus(null);
    setDomainAccounts([]);
    setFTPForm(initialFTPFormState);
    setFTPErrors({});
    setFTPLoadError(null);
    setAccountsLoadError(null);
    setFTPLoading(true);
    setAccountsLoading(true);

    async function loadFTPStatus() {
      const [statusResult, accountsResult] = await Promise.allSettled([
        fetchDomainFTPStatus(domain.id),
        fetchFTPAccounts(),
      ]);
      if (!active) {
        return;
      }

      let nextStatus: DomainFTPStatus | null = null;

      if (statusResult.status === "fulfilled") {
        nextStatus = statusResult.value;
        setFTPStatus(statusResult.value);
        setFTPForm({
          username: statusResult.value.username,
          password: "",
          enabled: statusResult.value.enabled,
        });
      } else {
        setFTPStatus(null);
        setFTPLoadError(
          getErrorMessage(
            statusResult.reason,
            `Failed to load FTP settings for ${domain.hostname}.`,
          ),
        );
      }

      if (accountsResult.status === "fulfilled") {
        setDomainAccounts(
          getDomainAccounts(accountsResult.value.accounts, domain.id, nextStatus),
        );
      } else {
        setDomainAccounts([]);
        setAccountsLoadError(
          getErrorMessage(
            accountsResult.reason,
            `Failed to load FTP accounts for ${domain.hostname}.`,
          ),
        );
      }

      setFTPLoading(false);
      setAccountsLoading(false);
    }

    void loadFTPStatus();

    return () => {
      active = false;
    };
  }, [domain, open]);

  async function reloadDomainAccounts(
    domainID: string,
    status: DomainFTPStatus | null,
    hostname: string,
  ) {
    setAccountsLoading(true);

    try {
      const payload = await fetchFTPAccounts();
      setDomainAccounts(getDomainAccounts(payload.accounts, domainID, status));
      setAccountsLoadError(null);
    } catch (error) {
      setDomainAccounts([]);
      setAccountsLoadError(
        getErrorMessage(error, `Failed to load FTP accounts for ${hostname}.`),
      );
    } finally {
      setAccountsLoading(false);
    }
  }

  async function handleSaveFTP() {
    if (!domain) {
      return;
    }

    const username = ftpForm.username.trim().toLowerCase();
    const nextErrors: FTPFormErrors = {
      username: username ? undefined : "FTP username is required.",
    };
    setFTPErrors(nextErrors);
    if (nextErrors.username) {
      return;
    }

    setFTPSaving(true);
    setFTPLoadError(null);

    try {
      const status = await updateDomainFTP(domain.id, {
        username,
        password: ftpForm.password,
        enabled: ftpForm.enabled,
      });
      setFTPStatus(status);
      setFTPForm({
        username: status.username,
        password: "",
        enabled: status.enabled,
      });
      await reloadDomainAccounts(domain.id, status, domain.hostname);
      toast.success(`Saved FTP settings for ${domain.hostname}.`);
      onOpenChange(false);
    } catch (error) {
      const domainError = error as DomainApiError;
      setFTPErrors({
        username: domainError.fieldErrors?.username,
        password: domainError.fieldErrors?.password,
        enabled: domainError.fieldErrors?.enabled,
      });
      setFTPLoadError(
        getErrorMessage(error, `Failed to save FTP settings for ${domain.hostname}.`),
      );
    } finally {
      setFTPSaving(false);
    }
  }

  const busy = ftpSaving;

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && busy) {
          return;
        }

        onOpenChange(nextOpen);
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{domain ? `${domain.hostname} FTP` : "FTP"}</DialogTitle>
        </DialogHeader>

        {ftpLoadError ? (
          <section className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
            {ftpLoadError}
          </section>
        ) : null}

        {ftpLoading ? (
          <div className="flex min-h-40 items-center justify-center text-sm text-[var(--app-text-muted)]">
            <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />
            Loading FTP account
          </div>
        ) : ftpStatus && !ftpStatus.supported ? (
          <div className="space-y-3 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-4">
            <p className="text-sm text-[var(--app-text)]">
              FTP is not available for this domain.
            </p>
            <p className="text-sm text-[var(--app-text-muted)]">
              This domain could not be attached to a managed document root.
            </p>
          </div>
        ) : ftpStatus ? (
          <div className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label>FTP server</Label>
                <span className="inline-flex min-h-8 items-center rounded-md border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-2.5 font-mono text-[13px] text-[var(--app-text)]">
                  {ftpStatus.host}
                </span>
              </div>

              <div className="space-y-2">
                <Label>FTP port</Label>
                <span className="inline-flex min-h-8 items-center rounded-md border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-2.5 font-mono text-[13px] text-[var(--app-text)]">
                  {ftpStatus.port}
                </span>
              </div>
            </div>

            <div className="space-y-2">
              <Label>Domain FTP accounts</Label>
              {accountsLoading ? (
                <div className="flex min-h-24 items-center justify-center rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] text-sm text-[var(--app-text-muted)]">
                  <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />
                  Loading linked accounts
                </div>
              ) : accountsLoadError ? (
                <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
                  {accountsLoadError}
                </div>
              ) : domainAccounts.length > 0 ? (
                <div className="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
                  {domainAccounts.map((account) => {
                    const managedAccount =
                      account.username === ftpStatus.username &&
                      account.root_path === ftpStatus.root_path;

                    return (
                      <div
                        key={account.id}
                        className="flex items-start justify-between gap-4 border-b border-[var(--app-border)] px-4 py-3 last:border-b-0"
                      >
                        <div className="min-w-0 space-y-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="font-mono text-[13px] text-[var(--app-text)]">
                              {account.username}
                            </p>
                            {managedAccount ? (
                              <span className="rounded-md border border-[var(--app-border)] px-1.5 py-0.5 text-[11px] font-medium text-[var(--app-text-muted)]">
                                Managed
                              </span>
                            ) : null}
                          </div>
                          <p className="break-all font-mono text-[12px] text-[var(--app-text-muted)]">
                            {account.root_path}
                          </p>
                        </div>
                        <div className="shrink-0 text-right text-[12px] text-[var(--app-text-muted)]">
                          <p>{account.enabled ? "Enabled" : "Disabled"}</p>
                          <p>{account.has_password ? "Password set" : "No password"}</p>
                        </div>
                      </div>
                    );
                  })}
                </div>
              ) : (
                <div className="rounded-lg border border-dashed border-[var(--app-border)] px-4 py-3 text-[13px] text-[var(--app-text-muted)]">
                  No FTP accounts are linked to this domain.
                </div>
              )}
              <p className="text-[12px] text-[var(--app-text-muted)]">
                Accounts assigned from the FTP Accounts page appear here alongside the
                managed domain FTP user.
              </p>
            </div>

            <hr className="border-[var(--app-border)]" />

            <div className="space-y-2">
              <Label htmlFor="domain-ftp-username">FTP username</Label>
              <Input
                id="domain-ftp-username"
                value={ftpForm.username}
                onChange={(event) => {
                  setFTPForm((current) => ({
                    ...current,
                    username: event.target.value,
                  }));
                  if (ftpErrors.username) {
                    setFTPErrors((current) => ({
                      ...current,
                      username: undefined,
                    }));
                  }
                }}
                autoComplete="off"
                spellCheck={false}
                aria-invalid={ftpErrors.username ? "true" : "false"}
                className={ftpErrors.username ? "border-[var(--app-danger)]" : ""}
              />
              {ftpErrors.username ? (
                <p className="text-[12px] text-[var(--app-danger)]">
                  {ftpErrors.username}
                </p>
              ) : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="domain-ftp-password">FTP password</Label>
              <PasswordInput
                id="domain-ftp-password"
                value={ftpForm.password}
                onChange={(event) => {
                  setFTPForm((current) => ({
                    ...current,
                    password: event.target.value,
                  }));
                  if (ftpErrors.password || ftpErrors.enabled) {
                    setFTPErrors((current) => ({
                      ...current,
                      password: undefined,
                      enabled: undefined,
                    }));
                  }
                }}
                onGeneratePassword={() => {
                  setFTPForm((current) => ({
                    ...current,
                    password: generateFTPPassword(),
                  }));
                  if (ftpErrors.password || ftpErrors.enabled) {
                    setFTPErrors((current) => ({
                      ...current,
                      password: undefined,
                      enabled: undefined,
                    }));
                  }
                }}
                autoComplete="new-password"
                aria-invalid={ftpErrors.password ? "true" : "false"}
                disabled={ftpLoading || busy}
              />
              <p className="text-[12px] text-[var(--app-text-muted)]">
                {ftpStatus.has_password
                  ? "Leave blank to keep the current password."
                  : "Set a password before enabling FTP."}
              </p>
              {ftpErrors.password ? (
                <p className="text-[12px] text-[var(--app-danger)]">
                  {ftpErrors.password}
                </p>
              ) : null}
            </div>

            <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-4">
              <div className="space-y-1">
                <Label htmlFor="domain-ftp-enabled">FTP account enabled</Label>
                <p className="text-sm text-[var(--app-text-muted)]">
                  Disabled accounts cannot log in, even if a password exists.
                </p>
                {ftpErrors.enabled ? (
                  <p className="text-[12px] text-[var(--app-danger)]">
                    {ftpErrors.enabled}
                  </p>
                ) : null}
              </div>
              <Switch
                id="domain-ftp-enabled"
                checked={ftpForm.enabled}
                onCheckedChange={(checked) => {
                  setFTPForm((current) => ({
                    ...current,
                    enabled: checked,
                  }));
                  if (ftpErrors.enabled) {
                    setFTPErrors((current) => ({
                      ...current,
                      enabled: undefined,
                    }));
                  }
                }}
              />
            </div>
          </div>
        ) : null}

        <DialogFooter className="border-t border-[var(--app-border)] pt-4">
          <div className="flex items-center justify-end gap-2">
            <Button
              type="button"
              variant="secondary"
              onClick={() => {
                onOpenChange(false);
              }}
              disabled={busy}
            >
              Close
            </Button>
            {ftpStatus?.supported ? (
              <Button
                type="button"
                onClick={() => {
                  void handleSaveFTP();
                }}
                disabled={ftpLoading || busy}
              >
                {ftpSaving ? "Saving..." : "Save FTP"}
              </Button>
            ) : null}
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
