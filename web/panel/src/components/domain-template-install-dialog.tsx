import { useEffect, useState } from "react";
import {
  installDomainTemplate,
  type DomainApiError,
  type DomainTemplateKey,
  type InstallDomainTemplateInput,
  type InstallDomainTemplateResult,
} from "@/api/domains";
import {
  LoaderCircle,
} from "@/components/icons/tabler-icons";
import { PasswordInput } from "@/components/password-input";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { toast } from "sonner";

type DomainTemplateInstallDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hostname: string;
  documentRoot: string;
  onInstalled?: (result: InstallDomainTemplateResult) => void | Promise<void>;
};

type InstallFormState = InstallDomainTemplateInput;

const generatedPasswordLength = 20;

const templateOptions: Array<{
  value: DomainTemplateKey;
  label: string;
  description: string;
  packageName?: string;
}> = [
  {
    value: "wordpress",
    label: "WordPress",
    description:
      "Install WordPress with WP-CLI and provision a MariaDB database automatically.",
  },
  {
    value: "symfony",
    label: "Symfony",
    description:
      "Create a Symfony project with Composer and install the standard webapp package set.",
    packageName: "symfony/skeleton",
  },
  {
    value: "laravel",
    label: "Laravel",
    description:
      "Create a fresh Laravel application with Composer and generate the app key.",
    packageName: "laravel/laravel",
  },
  {
    value: "octobercms",
    label: "October CMS",
    description:
      "Create an October CMS project, provision a MariaDB database, and run the initial migrations.",
    packageName: "october/october",
  },
  {
    value: "cakephp",
    label: "CakePHP",
    description: "Create a fresh CakePHP application with Composer.",
    packageName: "cakephp/app",
  },
  {
    value: "codeigniter",
    label: "CodeIgniter",
    description: "Create a CodeIgniter 4 starter project and set the base URL.",
    packageName: "codeigniter4/appstarter",
  },
  {
    value: "slim",
    label: "Slim",
    description: "Create the Slim skeleton application with Composer.",
    packageName: "slim/slim-skeleton",
  },
];

function suggestWordPressDatabaseName(hostname: string) {
  return suggestTemplateDatabaseName(hostname, "wp");
}

function suggestOctoberCMSDatabaseName(hostname: string) {
  return suggestTemplateDatabaseName(hostname, "october");
}

function suggestTemplateDatabaseName(hostname: string, prefix: string) {
  const normalized = hostname
    .trim()
    .toLowerCase()
    .replace(/\.$/, "")
    .replace(/^www\./, "");

  if (!normalized) {
    return `${prefix}_site`;
  }

  const sanitized = normalized
    .replace(/[.-]/g, "_")
    .replace(/[^a-z0-9_]/g, "_")
    .replace(/_+/g, "_")
    .replace(/^_+|_+$/g, "");

  return `${prefix}_${sanitized || "site"}`;
}

function getSuggestedDatabaseName(
  template: DomainTemplateKey,
  hostname: string,
) {
  switch (template) {
    case "wordpress":
      return suggestWordPressDatabaseName(hostname);
    case "octobercms":
      return suggestOctoberCMSDatabaseName(hostname);
    default:
      return "";
  }
}

function createInstallForm(hostname: string): InstallFormState {
  return {
    template: "wordpress",
    clear_document_root: true,
    app_name: hostname,
    database_name: suggestWordPressDatabaseName(hostname),
    site_title: hostname,
    admin_username: "admin",
    admin_email: hostname ? `admin@${hostname}` : "",
    admin_password: "",
    table_prefix: "wp_",
  };
}

function generatePassword(length = generatedPasswordLength) {
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

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

export function DomainTemplateInstallDialog({
  open,
  onOpenChange,
  hostname,
  documentRoot,
  onInstalled,
}: DomainTemplateInstallDialogProps) {
  const [form, setForm] = useState<InstallFormState>(() =>
    createInstallForm(hostname),
  );
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [installing, setInstalling] = useState(false);
  const [confirmInstallOpen, setConfirmInstallOpen] = useState(false);

  useEffect(() => {
    if (!open) {
      setForm(createInstallForm(hostname));
      setFieldErrors({});
      setError(null);
      setInstalling(false);
      setConfirmInstallOpen(false);
      return;
    }

    setForm(createInstallForm(hostname));
    setFieldErrors({});
    setError(null);
    setInstalling(false);
    setConfirmInstallOpen(false);
  }, [hostname, open]);

  const selectedTemplate =
    templateOptions.find((option) => option.value === form.template) ??
    templateOptions[0];
  const isWordPress = form.template === "wordpress";
  const isOctoberCMS = form.template === "octobercms";
  const showAppName =
    form.template === "laravel" ||
    form.template === "slim" ||
    form.template === "octobercms";

  function clearFieldError(field: string) {
    setFieldErrors((current) => {
      if (!(field in current)) {
        return current;
      }

      const next = { ...current };
      delete next[field];
      return next;
    });
  }

  function updateForm<K extends keyof InstallFormState>(
    field: K,
    value: InstallFormState[K],
  ) {
    setError(null);
    clearFieldError(String(field));
    setForm((current) => ({
      ...current,
      [field]: value,
    }));
  }

  function updateTemplate(template: DomainTemplateKey) {
    setError(null);
    setFieldErrors((current) => {
      if (!current.template && !current.database_name) {
        return current;
      }

      const next = { ...current };
      delete next.template;
      delete next.database_name;
      return next;
    });
    const previousTemplate = form.template;
    setForm((current) => ({
      ...current,
      template,
      database_name: (() => {
        const currentValue = (current.database_name ?? "").trim();
        const previousSuggestion = getSuggestedDatabaseName(
          previousTemplate,
          hostname,
        );
        const nextSuggestion = getSuggestedDatabaseName(template, hostname);

        if (!nextSuggestion) {
          return current.database_name;
        }

        if (!currentValue || currentValue === previousSuggestion) {
          return nextSuggestion;
        }

        return current.database_name;
      })(),
    }));
  }

  async function handleInstall() {
    setConfirmInstallOpen(false);
    setInstalling(true);
    setError(null);
    setFieldErrors({});

    try {
      const result = await installDomainTemplate(hostname, form);
      await onInstalled?.(result);
      toast.success(`${selectedTemplate.label} installed for ${hostname}.`);
      onOpenChange(false);
    } catch (installError) {
      const domainError = installError as DomainApiError;
      setFieldErrors(domainError.fieldErrors ?? {});
      const message = getErrorMessage(
        installError,
        `Failed to install ${selectedTemplate.label}.`,
      );
      setError(message);
      toast.error(message);
    } finally {
      setInstalling(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-4 sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{hostname} Install PHP App</DialogTitle>
          <DialogDescription>
            Choose a PHP application, review the generated inputs, and install
            it into this domain&apos;s document root.
          </DialogDescription>
        </DialogHeader>

        <div className="max-h-[80vh] space-y-4 overflow-y-auto pr-1">
          {error ? (
            <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
              {error}
            </div>
          ) : null}

          <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
            <div className="space-y-2">
              <Label htmlFor="domain_template_type">Application</Label>
              <Select
                value={form.template}
                onValueChange={(value) => {
                  updateTemplate(value as DomainTemplateKey);
                }}
                disabled={installing}
              >
                <SelectTrigger id="domain_template_type" className="w-full">
                  <SelectValue placeholder="Select an application" />
                </SelectTrigger>
                <SelectContent>
                  {templateOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {fieldErrors.template ? (
                <p className="text-[12px] text-[var(--app-danger)]">
                  {fieldErrors.template}
                </p>
              ) : null}
            </div>
          </section>

          <section className="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
            <div className="space-y-1">
              <div className="text-sm font-medium text-[var(--app-text)]">
                {selectedTemplate.label} settings
              </div>
              <p className="text-[13px] leading-5 text-[var(--app-text-muted)]">
                {selectedTemplate.description}
              </p>
            </div>

            {isWordPress ? (
              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="template_database_name">Database name</Label>
                  <Input
                    id="template_database_name"
                    value={form.database_name ?? ""}
                    disabled={installing}
                    onChange={(event) => {
                      updateForm("database_name", event.target.value);
                    }}
                  />
                  <p className="text-[12px] leading-5 text-[var(--app-text-muted)]">
                    FlowPanel will create the database, user, and password
                    automatically.
                  </p>
                  {fieldErrors.database_name ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {fieldErrors.database_name}
                    </p>
                  ) : null}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="template_table_prefix">Table prefix</Label>
                  <Input
                    id="template_table_prefix"
                    value={form.table_prefix ?? ""}
                    disabled={installing}
                    onChange={(event) => {
                      updateForm("table_prefix", event.target.value);
                    }}
                  />
                  {fieldErrors.table_prefix ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {fieldErrors.table_prefix}
                    </p>
                  ) : null}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="template_site_title">Site title</Label>
                  <Input
                    id="template_site_title"
                    value={form.site_title ?? ""}
                    disabled={installing}
                    onChange={(event) => {
                      updateForm("site_title", event.target.value);
                    }}
                  />
                  {fieldErrors.site_title ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {fieldErrors.site_title}
                    </p>
                  ) : null}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="template_admin_username">
                    Admin username
                  </Label>
                  <Input
                    id="template_admin_username"
                    value={form.admin_username ?? ""}
                    disabled={installing}
                    onChange={(event) => {
                      updateForm("admin_username", event.target.value);
                    }}
                  />
                  {fieldErrors.admin_username ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {fieldErrors.admin_username}
                    </p>
                  ) : null}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="template_admin_email">Admin email</Label>
                  <Input
                    id="template_admin_email"
                    type="email"
                    value={form.admin_email ?? ""}
                    disabled={installing}
                    onChange={(event) => {
                      updateForm("admin_email", event.target.value);
                    }}
                  />
                  {fieldErrors.admin_email ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {fieldErrors.admin_email}
                    </p>
                  ) : null}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="template_admin_password">
                    Admin password
                  </Label>
                  <PasswordInput
                    id="template_admin_password"
                    value={form.admin_password ?? ""}
                    disabled={installing}
                    onChange={(event) => {
                      updateForm("admin_password", event.target.value);
                    }}
                    onGeneratePassword={() => {
                      updateForm("admin_password", generatePassword());
                    }}
                  />
                  {fieldErrors.admin_password ? (
                    <p className="text-[12px] text-[var(--app-danger)]">
                      {fieldErrors.admin_password}
                    </p>
                  ) : null}
                </div>

                <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] px-3 py-3 text-[13px] leading-6 text-[var(--app-text-muted)] md:col-span-2">
                  {`FlowPanel will download WordPress, use https://${hostname} as the site URL, create the database automatically, and finish the WP-CLI install with your admin details.`}
                </div>
              </div>
            ) : (
              <div className="grid gap-4 md:grid-cols-2">
                {showAppName ? (
                  <div className="space-y-2">
                    <Label htmlFor="template_app_name">Application name</Label>
                    <Input
                      id="template_app_name"
                      value={form.app_name ?? ""}
                      disabled={installing}
                      onChange={(event) => {
                        updateForm("app_name", event.target.value);
                      }}
                    />
                    {fieldErrors.app_name ? (
                      <p className="text-[12px] text-[var(--app-danger)]">
                        {fieldErrors.app_name}
                      </p>
                    ) : null}
                  </div>
                ) : null}

                {isOctoberCMS ? (
                  <div className="space-y-2">
                    <Label htmlFor="template_october_database_name">
                      Database name
                    </Label>
                    <Input
                      id="template_october_database_name"
                      value={form.database_name ?? ""}
                      disabled={installing}
                      onChange={(event) => {
                        updateForm("database_name", event.target.value);
                      }}
                    />
                    <p className="text-[12px] leading-5 text-[var(--app-text-muted)]">
                      FlowPanel will create the MariaDB database, username, and
                      password automatically.
                    </p>
                    {fieldErrors.database_name ? (
                      <p className="text-[12px] text-[var(--app-danger)]">
                        {fieldErrors.database_name}
                      </p>
                    ) : null}
                  </div>
                ) : null}

                <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] px-3 py-3 text-[13px] leading-6 text-[var(--app-text-muted)] md:col-span-2">
                  {form.template === "symfony"
                    ? "FlowPanel will create the Symfony skeleton with Composer, install the standard webapp packages, and keep the generated project structure intact."
                    : form.template === "laravel"
                      ? `FlowPanel will create the project with Composer, set APP_NAME, use https://${hostname} as APP_URL, and generate the Laravel app key.`
                      : form.template === "octobercms"
                        ? `FlowPanel will create the October CMS project with Composer, set APP_NAME and https://${hostname} as APP_URL, provision the database automatically, generate the app key, and run the initial October migrations.`
                        : form.template === "cakephp"
                          ? "FlowPanel will create the CakePHP application with Composer and keep the generated project structure intact."
                          : form.template === "codeigniter"
                            ? `FlowPanel will create the project with Composer and write https://${hostname}/ into the generated .env file as the base URL.`
                            : "FlowPanel will create the Slim skeleton with Composer, set APP_NAME, and keep the generated project structure intact."}
                </div>
              </div>
            )}

            <div className="flex justify-end">
              <Button
                type="button"
                disabled={installing}
                onClick={() => {
                  setConfirmInstallOpen(true);
                }}
              >
                {installing ? (
                  <>
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                    Installing...
                  </>
                ) : (
                  "Install app"
                )}
              </Button>
            </div>
          </section>
        </div>
      </DialogContent>

      <ActionConfirmDialog
        open={confirmInstallOpen}
        onOpenChange={(nextOpen) => {
          if (!nextOpen && !installing) {
            setConfirmInstallOpen(false);
          }
        }}
        title={`Install ${selectedTemplate.label}`}
        desc={`Installing ${selectedTemplate.label} will delete the current site content in ${documentRoot || "the document root"} before the new application is copied.`}
        confirmText="Install app"
        destructive
        isLoading={installing}
        handleConfirm={() => {
          void handleInstall();
        }}
        className="sm:max-w-md"
      />
    </Dialog>
  );
}
