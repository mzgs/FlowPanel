import { useEffect, useState } from "react";
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

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
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
  const [ftpForm, setFTPForm] = useState<FTPFormState>(initialFTPFormState);
  const [ftpErrors, setFTPErrors] = useState<FTPFormErrors>({});
  const [ftpLoadError, setFTPLoadError] = useState<string | null>(null);
  const [ftpLoading, setFTPLoading] = useState(false);
  const [ftpSaving, setFTPSaving] = useState(false);

  useEffect(() => {
    if (!open || !domain) {
      setFTPStatus(null);
      setFTPForm(initialFTPFormState);
      setFTPErrors({});
      setFTPLoadError(null);
      setFTPLoading(false);
      setFTPSaving(false);
      return;
    }

    let active = true;
    setFTPStatus(null);
    setFTPForm(initialFTPFormState);
    setFTPErrors({});
    setFTPLoadError(null);
    setFTPLoading(true);

    async function loadFTPStatus() {
      try {
        const status = await fetchDomainFTPStatus(domain.id);
        if (!active) {
          return;
        }

        setFTPStatus(status);
        setFTPForm({
          username: status.username,
          password: "",
          enabled: status.enabled,
        });
      } catch (error) {
        if (!active) {
          return;
        }

        setFTPStatus(null);
        setFTPLoadError(
          getErrorMessage(error, `Failed to load FTP settings for ${domain.hostname}.`),
        );
      } finally {
        if (active) {
          setFTPLoading(false);
        }
      }
    }

    void loadFTPStatus();

    return () => {
      active = false;
    };
  }, [domain, open]);

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
              FTP is available only for Static site and Php site domains.
            </p>
            <p className="text-sm text-[var(--app-text-muted)]">
              This domain does not have a managed document root, so there is no FTP
              sandbox to attach an account to.
            </p>
          </div>
        ) : ftpStatus ? (
          <div className="space-y-4">
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
