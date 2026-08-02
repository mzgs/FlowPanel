import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import {
  fetchAlertSettings,
  sendTestAlert,
  updateAlertSettings,
  type AlertSettings,
  type AlertSettingsApiError,
} from "@/api/alerts";
import { Bell, LoaderCircle, RefreshCw } from "@/components/icons/lucide-icons";
import { FieldError } from "@/components/field-error";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";

type FormState = {
  enabled: boolean;
  webhook_url: string;
  webhook_secret: string;
  smtp_host: string;
  smtp_port: string;
  smtp_encryption: "none" | "starttls" | "tls";
  smtp_username: string;
  smtp_password: string;
  smtp_from: string;
  smtp_recipients: string;
  disk_warning_percent: string;
  disk_critical_percent: string;
  certificate_warning_days: string;
  login_failure_threshold: string;
  cooldown_minutes: string;
  notify_recovery: boolean;
};

const emptyForm: FormState = {
  enabled: false,
  webhook_url: "",
  webhook_secret: "",
  smtp_host: "",
  smtp_port: "587",
  smtp_encryption: "starttls",
  smtp_username: "",
  smtp_password: "",
  smtp_from: "",
  smtp_recipients: "",
  disk_warning_percent: "85",
  disk_critical_percent: "95",
  certificate_warning_days: "7",
  login_failure_threshold: "10",
  cooldown_minutes: "360",
  notify_recovery: true,
};

function toForm(settings: AlertSettings): FormState {
  return {
    enabled: settings.enabled,
    webhook_url: settings.webhook_url,
    webhook_secret: "",
    smtp_host: settings.smtp_host,
    smtp_port: String(settings.smtp_port),
    smtp_encryption: settings.smtp_encryption,
    smtp_username: settings.smtp_username,
    smtp_password: "",
    smtp_from: settings.smtp_from,
    smtp_recipients: settings.smtp_recipients,
    disk_warning_percent: String(settings.disk_warning_percent),
    disk_critical_percent: String(settings.disk_critical_percent),
    certificate_warning_days: String(settings.certificate_warning_days),
    login_failure_threshold: String(settings.login_failure_threshold),
    cooldown_minutes: String(settings.cooldown_minutes),
    notify_recovery: settings.notify_recovery,
  };
}

export function NotificationSettings() {
  const [form, setForm] = useState<FormState>(emptyForm);
  const [savedForm, setSavedForm] = useState<FormState | null>(null);
  const [settings, setSettings] = useState<AlertSettings | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [loadError, setLoadError] = useState("");
  const dirty = useMemo(() => savedForm !== null && JSON.stringify(form) !== JSON.stringify(savedForm), [form, savedForm]);

  async function load() {
    setLoading(true);
    setLoadError("");
    try {
      const next = await fetchAlertSettings();
      const nextForm = toForm(next);
      setSettings(next);
      setForm(nextForm);
      setSavedForm(nextForm);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Failed to load notification settings.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  function update<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((current) => ({ ...current, [key]: value }));
    if (fieldErrors[key]) {
      setFieldErrors((current) => {
        const next = { ...current };
        delete next[key];
        return next;
      });
    }
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setFieldErrors({});
    try {
      const next = await updateAlertSettings({
        enabled: form.enabled,
        webhook_url: form.webhook_url,
        webhook_secret: form.webhook_secret,
        smtp_host: form.smtp_host,
        smtp_port: Number.parseInt(form.smtp_port, 10) || 0,
        smtp_encryption: form.smtp_encryption,
        smtp_username: form.smtp_username,
        smtp_password: form.smtp_password,
        smtp_from: form.smtp_from,
        smtp_recipients: form.smtp_recipients,
        disk_warning_percent: Number.parseInt(form.disk_warning_percent, 10) || 0,
        disk_critical_percent: Number.parseInt(form.disk_critical_percent, 10) || 0,
        certificate_warning_days: Number.parseInt(form.certificate_warning_days, 10) || 0,
        login_failure_threshold: Number.parseInt(form.login_failure_threshold, 10) || 0,
        cooldown_minutes: Number.parseInt(form.cooldown_minutes, 10) || 0,
        notify_recovery: form.notify_recovery,
      });
      const nextForm = toForm(next);
      setSettings(next);
      setForm(nextForm);
      setSavedForm(nextForm);
      toast.success("Notification settings saved.");
    } catch (error) {
      const apiError = error as AlertSettingsApiError;
      setFieldErrors(apiError.fieldErrors ?? {});
      toast.error(apiError.message || "Notification settings could not be saved.");
    } finally {
      setSaving(false);
    }
  }

  async function test() {
    setTesting(true);
    try {
      await sendTestAlert();
      toast.success("Test notification sent.");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Test notification failed.");
    } finally {
      setTesting(false);
    }
  }

  if (loading) {
    return <section className="mt-4 flex h-24 items-center justify-center rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)]"><LoaderCircle className="h-4 w-4 animate-spin" /></section>;
  }
  if (loadError) {
    return <section className="mt-4 flex items-center justify-between gap-4 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] px-5 py-4"><p className="text-sm text-destructive">{loadError}</p><Button type="button" variant="outline" onClick={() => void load()}><RefreshCw className="h-4 w-4" />Retry</Button></section>;
  }

  return (
    <form onSubmit={save} className="mt-4 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] shadow-sm">
      <div className="flex items-center justify-between gap-4 border-b border-[var(--app-border)] px-5 py-4">
        <div className="flex gap-3">
          <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary"><Bell className="h-4 w-4" /></div>
          <div>
            <div className="flex items-center gap-2"><h2 className="text-sm font-semibold">Notifications</h2><Badge variant={form.enabled ? "default" : "secondary"}>{form.enabled ? "Enabled" : "Disabled"}</Badge></div>
            <p className="mt-0.5 text-xs text-[var(--app-text-muted)]">Webhook and email alerts for disk, backups, login security, and certificates.</p>
          </div>
        </div>
        <Switch checked={form.enabled} onCheckedChange={(checked) => update("enabled", checked)} aria-label="Enable notifications" />
      </div>

      <div className="grid gap-6 px-5 py-4 lg:grid-cols-2">
        <div className="space-y-4">
          <div><h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--app-text-muted)]">Webhook</h3></div>
          <Field id="alert_webhook_url" label="Webhook URL" error={fieldErrors.webhook_url}><Input id="alert_webhook_url" value={form.webhook_url} onChange={(event) => update("webhook_url", event.target.value)} placeholder="https://example.com/hooks/flowpanel" spellCheck={false} /></Field>
          <Field id="alert_webhook_secret" label="Signing secret" error={fieldErrors.webhook_secret}><Input id="alert_webhook_secret" type="password" value={form.webhook_secret} onChange={(event) => update("webhook_secret", event.target.value)} placeholder={settings?.webhook_secret_configured ? "Configured — leave blank to keep" : "Optional HMAC secret"} autoComplete="new-password" /></Field>

          <div className="border-t border-[var(--app-border)] pt-4"><h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--app-text-muted)]">Alert rules</h3></div>
          <div className="grid gap-4 sm:grid-cols-2">
            <NumberField id="disk_warning_percent" label="Disk warning %" value={form.disk_warning_percent} min="1" max="99" error={fieldErrors.disk_warning_percent} onChange={(value) => update("disk_warning_percent", value)} />
            <NumberField id="disk_critical_percent" label="Disk critical %" value={form.disk_critical_percent} min="2" max="100" error={fieldErrors.disk_critical_percent} onChange={(value) => update("disk_critical_percent", value)} />
            <NumberField id="certificate_warning_days" label="Certificate warning days" value={form.certificate_warning_days} min="1" max="365" error={fieldErrors.certificate_warning_days} onChange={(value) => update("certificate_warning_days", value)} />
            <NumberField id="login_failure_threshold" label="Login failure threshold" value={form.login_failure_threshold} min="1" max="10" error={fieldErrors.login_failure_threshold} onChange={(value) => update("login_failure_threshold", value)} />
            <NumberField id="cooldown_minutes" label="Repeat cooldown minutes" value={form.cooldown_minutes} min="5" max="10080" error={fieldErrors.cooldown_minutes} onChange={(value) => update("cooldown_minutes", value)} />
            <div className="flex items-end"><label className="flex h-9 w-full items-center justify-between rounded-md border border-[var(--app-border)] px-3 text-sm"><span>Recovery alerts</span><Switch checked={form.notify_recovery} onCheckedChange={(checked) => update("notify_recovery", checked)} /></label></div>
          </div>
        </div>

        <div className="space-y-4">
          <div><h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--app-text-muted)]">Email (SMTP)</h3></div>
          <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_7rem]">
            <Field id="smtp_host" label="SMTP host" error={fieldErrors.smtp_host}><Input id="smtp_host" value={form.smtp_host} onChange={(event) => update("smtp_host", event.target.value)} placeholder="smtp.example.com" spellCheck={false} /></Field>
            <NumberField id="smtp_port" label="Port" value={form.smtp_port} min="1" max="65535" error={fieldErrors.smtp_port} onChange={(value) => update("smtp_port", value)} />
          </div>
          <Field id="smtp_encryption" label="Encryption" error={fieldErrors.smtp_encryption}>
            <Select value={form.smtp_encryption} onValueChange={(value: "none" | "starttls" | "tls") => update("smtp_encryption", value)}><SelectTrigger id="smtp_encryption" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="starttls">STARTTLS</SelectItem><SelectItem value="tls">Implicit TLS</SelectItem><SelectItem value="none">None</SelectItem></SelectContent></Select>
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field id="smtp_username" label="Username" error={fieldErrors.smtp_username}><Input id="smtp_username" value={form.smtp_username} onChange={(event) => update("smtp_username", event.target.value)} autoComplete="off" /></Field>
            <Field id="smtp_password" label="Password" error={fieldErrors.smtp_password}><Input id="smtp_password" type="password" value={form.smtp_password} onChange={(event) => update("smtp_password", event.target.value)} placeholder={settings?.smtp_password_configured ? "Configured — leave blank to keep" : "SMTP password"} autoComplete="new-password" /></Field>
          </div>
          <Field id="smtp_from" label="From address" error={fieldErrors.smtp_from}><Input id="smtp_from" type="email" value={form.smtp_from} onChange={(event) => update("smtp_from", event.target.value)} placeholder="flowpanel@example.com" /></Field>
          <Field id="smtp_recipients" label="Recipients" error={fieldErrors.smtp_recipients}><Input id="smtp_recipients" value={form.smtp_recipients} onChange={(event) => update("smtp_recipients", event.target.value)} placeholder="admin@example.com, ops@example.com" /></Field>
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
        <FieldError message={fieldErrors.channels || fieldErrors.enabled} />
        <Button type="button" variant="outline" disabled={testing || dirty || !form.enabled} onClick={() => void test()}>{testing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}{testing ? "Sending" : "Send test"}</Button>
        <Button type="submit" disabled={saving || !dirty}>{saving ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}{saving ? "Saving" : "Save notifications"}</Button>
      </div>
    </form>
  );
}

function Field({ id, label, error, children }: { id: string; label: string; error?: string; children: ReactNode }) {
  return <div className="space-y-2"><Label htmlFor={id}>{label}</Label>{children}<FieldError message={error} /></div>;
}

function NumberField({ id, label, value, min, max, error, onChange }: { id: string; label: string; value: string; min: string; max: string; error?: string; onChange: (value: string) => void }) {
  return <Field id={id} label={label} error={error}><Input id={id} type="number" min={min} max={max} value={value} onChange={(event) => onChange(event.target.value)} /></Field>;
}
