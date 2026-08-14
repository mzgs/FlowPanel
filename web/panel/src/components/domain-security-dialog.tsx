import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  clearDomainSecurityEvents,
  fetchDomainSecurityEvents,
  updateDomainProtection,
  type DomainRecord,
  type DomainSecurityEvent,
  type ProtectionConfig,
  type RateLimitPreset,
  type WAFMode,
  type WAFPathExclusion,
} from "@/api/domains";
import {
  ExternalLink,
  List,
  LoaderCircle,
  RefreshCw,
  ShieldCheck,
  Trash2,
} from "@/components/icons/lucide-icons";
import { ConfirmDialog } from "@/components/confirm-dialog";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { formatDateTime } from "@/lib/format";
import { getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

type SecurityTab = "waf" | "rate-limit" | "ip-rules" | "auto-ban" | "logs";

type SecurityForm = {
  wafMode: WAFMode;
  paranoiaLevel: string;
  excludedRuleIDs: string;
  disabledPaths: string;
  pathRuleExclusions: string;
  customRules: string;
  rateLimitEnabled: boolean;
  rateLimitPreset: RateLimitPreset;
  requestsPerMinute: string;
  allowedIPs: string;
  blockedIPs: string;
  autoBanEnabled: boolean;
  autoBanBlockedRequests: string;
  autoBanWindowMinutes: string;
  autoBanMinutes: string;
};

type DomainSecurityDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  domain: DomainRecord;
  onSaved: (domain: DomainRecord) => void;
};

const securityTabs: { value: SecurityTab; label: string }[] = [
  { value: "waf", label: "WAF" },
  { value: "rate-limit", label: "Rate limit" },
  { value: "ip-rules", label: "IP rules" },
  { value: "auto-ban", label: "Auto-ban" },
  { value: "logs", label: "Logs" },
];

const defaultProtectionConfig: ProtectionConfig = {
  waf: {
    mode: "disabled",
    paranoia_level: 1,
    excluded_rule_ids: [],
    path_exclusions: [],
    custom_rules: "",
  },
  rate_limit: {
    enabled: false,
    preset: "normal",
    requests_per_minute: 120,
  },
  ip_access: {
    allowed: [],
    blocked: [],
  },
  auto_ban: {
    enabled: false,
    blocked_requests: 20,
    window_minutes: 10,
    ban_minutes: 60,
  },
};

export function DomainSecurityDialog({
  open,
  onOpenChange,
  domain,
  onSaved,
}: DomainSecurityDialogProps) {
  const [activeTab, setActiveTab] = useState<SecurityTab>("waf");
  const [form, setForm] = useState<SecurityForm>(() =>
    formFromProtection(domain.protection_config),
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [securityEvents, setSecurityEvents] = useState<DomainSecurityEvent[]>([]);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logsClearing, setLogsClearing] = useState(false);
  const [logsError, setLogsError] = useState<string | null>(null);
  const [clearLogsConfirmOpen, setClearLogsConfirmOpen] = useState(false);

  useEffect(() => {
    if (!open) {
      return;
    }
    setActiveTab("waf");
    setForm(formFromProtection(domain.protection_config));
    setError(null);
    setClearLogsConfirmOpen(false);
  }, [domain.protection_config, open]);

  const loadSecurityEvents = useCallback(async () => {
    setLogsLoading(true);
    setLogsError(null);
    try {
      setSecurityEvents(await fetchDomainSecurityEvents(domain.hostname));
    } catch (loadError) {
      setLogsError(getErrorMessage(loadError, "Failed to load security events."));
    } finally {
      setLogsLoading(false);
    }
  }, [domain.hostname]);

  useEffect(() => {
    if (open && activeTab === "logs") {
      void loadSecurityEvents();
    }
  }, [activeTab, loadSecurityEvents, open]);

  const wafEnabled = form.wafMode !== "disabled";

  async function handleClearSecurityEvents() {
    setLogsClearing(true);
    try {
      const cleared = await clearDomainSecurityEvents(domain.hostname);
      setSecurityEvents([]);
      setLogsError(null);
      setClearLogsConfirmOpen(false);
      toast.success(`Cleared ${cleared} security log ${cleared === 1 ? "entry" : "entries"}.`);
    } catch (clearError) {
      toast.error(getErrorMessage(clearError, "Failed to clear security logs."));
    } finally {
      setLogsClearing(false);
    }
  }

  async function handleSave() {
    const validationError = validateSecurityForm(form);
    if (validationError) {
      setError(validationError);
      return;
    }
    setSaving(true);
    setError(null);

    try {
      const updatedDomain = await updateDomainProtection(domain.hostname, {
        protection_config: protectionFromForm(form),
      });
      onSaved(updatedDomain);
      toast.success("Security settings saved.");
      onOpenChange(false);
    } catch (saveError) {
      setError(
        firstFieldError(saveError) ??
          getErrorMessage(saveError, "Failed to save security settings."),
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={saving ? undefined : onOpenChange}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldCheck className="h-5 w-5" />
            Security
          </DialogTitle>
          <DialogDescription>{domain.hostname}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-wrap gap-1 border-b border-[var(--app-border)] pb-2">
          {securityTabs.map((tab) => (
            <button
              key={tab.value}
              type="button"
              className={`rounded-md px-2.5 py-1 text-sm font-medium ${
                activeTab === tab.value
                  ? "bg-[var(--app-surface-muted)] text-[var(--app-text)]"
                  : "text-[var(--app-text-muted)] hover:text-[var(--app-text)]"
              }`}
              onClick={() => setActiveTab(tab.value)}
            >
              {tab.label}
            </button>
          ))}
        </div>

        <div className="min-h-[390px] space-y-3">
          {activeTab === "waf" ? (
            <div className="grid gap-3 md:grid-cols-[1fr_1fr]">
              <Field label="Mode">
                <Select
                  value={form.wafMode}
                  onValueChange={(value) =>
                    setForm((current) => ({
                      ...current,
                      wafMode: value as WAFMode,
                    }))
                  }
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="disabled">Disabled</SelectItem>
                    <SelectItem value="detection_only">Detection only</SelectItem>
                    <SelectItem value="blocking">Blocking</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field label="Paranoia level">
                <Select
                  value={form.paranoiaLevel}
                  onValueChange={(value) =>
                    setForm((current) => ({ ...current, paranoiaLevel: value }))
                  }
                  disabled={!wafEnabled}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="1">1 Basic</SelectItem>
                    <SelectItem value="2">2 Balanced</SelectItem>
                    <SelectItem value="3">3 Strict</SelectItem>
                    <SelectItem value="4">4 Very strict</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              {form.wafMode === "detection_only" ? (
                <p className="text-xs text-[var(--app-text-muted)] md:col-span-2">
                  Detection mode reports matches without blocking requests and
                  does not trigger auto-ban.
                </p>
              ) : null}
              <Field label="Excluded rule IDs">
                <Input
                  value={form.excludedRuleIDs}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      excludedRuleIDs: event.target.value,
                    }))
                  }
                  placeholder="942100, 941100"
                  disabled={!wafEnabled}
                />
              </Field>
              <Field label="Disabled paths">
                <Textarea
                  className="min-h-20 resize-y text-sm"
                  value={form.disabledPaths}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      disabledPaths: event.target.value,
                    }))
                  }
                  placeholder="/webhook/github"
                  disabled={!wafEnabled}
                />
              </Field>
              <Field label="Path rule exclusions" className="md:col-span-2">
                <Textarea
                  className="min-h-20 resize-y font-mono text-sm"
                  value={form.pathRuleExclusions}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      pathRuleExclusions: event.target.value,
                    }))
                  }
                  placeholder="/api/search 942100,941100"
                  disabled={!wafEnabled}
                />
              </Field>
              <Field label="Custom directives" className="md:col-span-2">
                <Textarea
                  className="min-h-28 resize-y font-mono text-sm"
                  value={form.customRules}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      customRules: event.target.value,
                    }))
                  }
                  placeholder={`SecRule REQUEST_URI "@beginsWith /private" "id:100001,phase:1,deny,status:403,msg:'Blocked private path'"`}
                  disabled={!wafEnabled}
                />
                <a
                  href="https://www.coraza.io/docs/seclang/syntax/"
                  target="_blank"
                  rel="noreferrer"
                  className="mt-1.5 inline-flex items-center gap-1 text-xs text-blue-600 hover:text-blue-700"
                >
                  Coraza SecLang syntax documentation
                  <ExternalLink className="h-3.5 w-3.5" />
                </a>
              </Field>
            </div>
          ) : null}

          {activeTab === "rate-limit" ? (
            <div className="space-y-3">
              <ToggleRow
                label="Per-IP rate limit"
                checked={form.rateLimitEnabled}
                onCheckedChange={(checked) =>
                  setForm((current) => ({
                    ...current,
                    rateLimitEnabled: checked,
                  }))
                }
              />
              <div className="grid gap-3 md:grid-cols-2">
                <Field label="Preset">
                  <Select
                    value={form.rateLimitPreset}
                    onValueChange={(value) =>
                      setForm((current) => ({
                        ...current,
                        rateLimitPreset: value as RateLimitPreset,
                        requestsPerMinute:
                          value === "strict"
                            ? "60"
                            : value === "normal"
                              ? "120"
                              : current.requestsPerMinute,
                      }))
                    }
                    disabled={!form.rateLimitEnabled}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="normal">Normal</SelectItem>
                      <SelectItem value="strict">Strict</SelectItem>
                      <SelectItem value="custom">Custom</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
                <Field label="Requests per minute">
                  <Input
                    type="number"
                    min={1}
                    max={10000}
                    value={form.requestsPerMinute}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        rateLimitPreset: "custom",
                        requestsPerMinute: event.target.value,
                      }))
                    }
                    disabled={!form.rateLimitEnabled}
                  />
                </Field>
              </div>
              <SummaryRow
                label="Response"
                value={form.rateLimitEnabled ? "429 Too Many Requests" : "Off"}
              />
              <p className="text-xs text-[var(--app-text-muted)]">
                Page and API requests are counted. CSS, JavaScript, images,
                fonts, and media files are excluded.
              </p>
            </div>
          ) : null}

          {activeTab === "ip-rules" ? (
            <div className="grid gap-3 md:grid-cols-2">
              <Field label="Trusted IPs / CIDRs (bypass)">
                <Textarea
                  className="min-h-56 resize-y font-mono text-sm"
                  value={form.allowedIPs}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      allowedIPs: event.target.value,
                    }))
                  }
                  placeholder={`203.0.113.10\n2001:db8::/32`}
                />
              </Field>
              <Field label="Blocked IPs / CIDRs">
                <Textarea
                  className="min-h-56 resize-y font-mono text-sm"
                  value={form.blockedIPs}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      blockedIPs: event.target.value,
                    }))
                  }
                  placeholder={`198.51.100.24\n192.0.2.0/24`}
                />
              </Field>
              <p className="text-xs text-[var(--app-text-muted)] md:col-span-2">
                Trusted addresses bypass IP blocks, rate limits, and auto-ban.
                WAF inspection still applies.
              </p>
            </div>
          ) : null}

          {activeTab === "auto-ban" ? (
            <div className="space-y-3">
              <ToggleRow
                label="Auto-ban"
                checked={form.autoBanEnabled}
                onCheckedChange={(checked) =>
                  setForm((current) => ({ ...current, autoBanEnabled: checked }))
                }
              />
              <div className="grid gap-3 md:grid-cols-3">
                <Field label="Blocked requests">
                  <Input
                    type="number"
                    min={1}
                    max={10000}
                    value={form.autoBanBlockedRequests}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        autoBanBlockedRequests: event.target.value,
                      }))
                    }
                    disabled={!form.autoBanEnabled}
                  />
                </Field>
                <Field label="Window minutes">
                  <Input
                    type="number"
                    min={1}
                    max={1440}
                    value={form.autoBanWindowMinutes}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        autoBanWindowMinutes: event.target.value,
                      }))
                    }
                    disabled={!form.autoBanEnabled}
                  />
                </Field>
                <Field label="Ban minutes">
                  <Input
                    type="number"
                    min={1}
                    max={10080}
                    value={form.autoBanMinutes}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        autoBanMinutes: event.target.value,
                      }))
                    }
                    disabled={!form.autoBanEnabled}
                  />
                </Field>
              </div>
              <SummaryRow
                label="Status"
                value={
                  form.autoBanEnabled
                    ? `Ban after ${form.autoBanBlockedRequests} blocked requests`
                    : "Off"
                }
              />
              <p className="text-xs text-[var(--app-text-muted)]">
                Auto-ban counts requests blocked by blocking WAF or rate
                limiting. Detection-only WAF and IP block rules do not add
                strikes.
              </p>
            </div>
          ) : null}

          {activeTab === "logs" ? (
            <div className="overflow-hidden rounded-lg border border-[var(--app-border)]">
              <div className="flex items-center justify-between gap-3 border-b border-[var(--app-border)] px-3 py-2">
                <div>
                  <div className="text-sm font-medium">Blocked actions</div>
                  <div className="text-xs text-[var(--app-text-muted)]">
                    WAF, rate-limit, IP-rule, and auto-ban actions; repeated
                    actions are grouped for one minute
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-8"
                    onClick={() => setClearLogsConfirmOpen(true)}
                    disabled={logsLoading || logsClearing || securityEvents.length === 0}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    Clear logs
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-8"
                    onClick={() => void loadSecurityEvents()}
                    disabled={logsLoading || logsClearing}
                  >
                    <RefreshCw className={`h-3.5 w-3.5 ${logsLoading ? "animate-spin" : ""}`} />
                    Refresh
                  </Button>
                </div>
              </div>

              <div className="max-h-[320px] overflow-y-auto">
                {logsLoading && securityEvents.length === 0 ? (
                  <LogState icon={<LoaderCircle className="h-4 w-4 animate-spin" />} message="Loading security events..." />
                ) : logsError ? (
                  <LogState icon={<List className="h-4 w-4" />} message={logsError} />
                ) : securityEvents.length === 0 ? (
                  <LogState icon={<List className="h-4 w-4" />} message="No security events recorded." />
                ) : (
                  securityEvents.map((event) => (
                    <div key={event.id} className="grid gap-1 border-b border-[var(--app-border)] px-3 py-2.5 last:border-b-0 sm:grid-cols-[8rem_minmax(0,1fr)_9rem]">
                      <div className="text-xs text-[var(--app-text-muted)]">{formatDateTime(event.created_at)}</div>
                      <div className="min-w-0">
                        <div
                          className="truncate text-sm font-medium"
                          title={securityEventTitle(event)}
                        >
                          {securityEventTitle(event)}
                        </div>
                        <div className="truncate text-xs text-[var(--app-text-muted)]" title={securityEventMeta(event)}>{securityEventMeta(event)}</div>
                      </div>
                      <div className="flex items-start justify-between gap-2 sm:justify-end">
                        <span className="truncate text-xs text-[var(--app-text-muted)]" title={event.client_ip}>{event.client_ip}</span>
                        <Badge variant={event.action === "rate_limited" ? "secondary" : "destructive"}>{securityEventLabel(event.action)}</Badge>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </div>
          ) : null}
        </div>

        {error ? <p className="text-sm text-red-600">{error}</p> : null}

        <div className="flex items-center justify-between gap-3 border-t border-[var(--app-border)] pt-3">
          <div className="flex flex-wrap gap-2">
            <Badge variant={form.wafMode === "disabled" ? "secondary" : "default"}>
              WAF {form.wafMode === "disabled" ? "off" : form.wafMode === "blocking" ? "blocking" : "detecting"}
            </Badge>
            <Badge variant={form.rateLimitEnabled ? "default" : "secondary"}>
              Rate {form.rateLimitEnabled ? `${form.requestsPerMinute}/min` : "off"}
            </Badge>
            <Badge
              variant={
                lineList(form.blockedIPs).length > 0 ? "default" : "secondary"
              }
            >
              Blocked IPs {lineList(form.blockedIPs).length}
            </Badge>
          </div>
          <div className="flex gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={saving}
            >
              Cancel
            </Button>
            <Button type="button" onClick={handleSave} disabled={saving}>
              {saving ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              Save
            </Button>
          </div>
        </div>
      </DialogContent>
      <ConfirmDialog
        open={clearLogsConfirmOpen}
        onOpenChange={setClearLogsConfirmOpen}
        title="Clear security logs?"
        desc={`Delete all recorded security events for ${domain.hostname}? Active auto-bans will remain in effect.`}
        confirmText="Clear logs"
        destructive
        isLoading={logsClearing}
        handleConfirm={() => void handleClearSecurityEvents()}
      />
    </Dialog>
  );
}

function LogState({ icon, message }: { icon: ReactNode; message: string }) {
  return (
    <div className="flex min-h-56 items-center justify-center gap-2 text-sm text-[var(--app-text-muted)]">
      {icon}
      {message}
    </div>
  );
}

function securityEventLabel(action: DomainSecurityEvent["action"]) {
  switch (action) {
    case "rate_limited":
      return "Rate limited";
    case "ip_blocked":
      return "IP blocked";
    case "auto_banned":
      return "Auto-banned";
    case "auto_ban_blocked":
      return "Ban enforced";
    default:
      return "WAF blocked";
  }
}

function securityEventMeta(event: DomainSecurityEvent) {
  if (event.expires_at) {
    return `Ban until ${formatDateTime(event.expires_at)}`;
  }
  return event.transaction_id && event.transaction_id !== "-"
    ? `ID ${event.transaction_id}`
    : securityEventLabel(event.action);
}

function securityEventTitle(event: DomainSecurityEvent) {
  return event.uri && event.uri !== "-"
    ? event.uri
    : securityEventLabel(event.action);
}

function Field({
  label,
  className,
  children,
}: {
  label: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div className={className}>
      <Label className="mb-1.5 text-xs text-[var(--app-text-muted)]">
        {label}
      </Label>
      {children}
    </div>
  );
}

function ToggleRow({
  label,
  checked,
  onCheckedChange,
}: {
  label: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-[var(--app-border)] px-3 py-2">
      <span className="text-sm font-medium text-[var(--app-text)]">{label}</span>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}

function SummaryRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-[var(--app-border)] px-3 py-2 text-sm">
      <span className="text-[var(--app-text-muted)]">{label}</span>
      <span className="font-medium text-[var(--app-text)]">{value}</span>
    </div>
  );
}

function normalizeProtection(config: ProtectionConfig | undefined): ProtectionConfig {
  return {
    ...defaultProtectionConfig,
    ...config,
    waf: { ...defaultProtectionConfig.waf, ...config?.waf },
    rate_limit: {
      ...defaultProtectionConfig.rate_limit,
      ...config?.rate_limit,
    },
    ip_access: {
      ...defaultProtectionConfig.ip_access,
      ...config?.ip_access,
    },
    auto_ban: {
      ...defaultProtectionConfig.auto_ban,
      ...config?.auto_ban,
    },
  };
}

function formFromProtection(config: ProtectionConfig | undefined): SecurityForm {
  const protection = normalizeProtection(config);
  const disabledPaths: string[] = [];
  const pathRuleExclusions: string[] = [];

  for (const exclusion of protection.waf.path_exclusions ?? []) {
    if (exclusion.disable_waf) {
      disabledPaths.push(exclusion.path);
    } else if (exclusion.excluded_rule_ids.length > 0) {
      pathRuleExclusions.push(
        `${exclusion.path} ${exclusion.excluded_rule_ids.join(",")}`,
      );
    }
  }

  return {
    wafMode: protection.waf.mode,
    paranoiaLevel: String(protection.waf.paranoia_level || 1),
    excludedRuleIDs: protection.waf.excluded_rule_ids.join(", "),
    disabledPaths: disabledPaths.join("\n"),
    pathRuleExclusions: pathRuleExclusions.join("\n"),
    customRules: protection.waf.custom_rules,
    rateLimitEnabled: protection.rate_limit.enabled,
    rateLimitPreset: protection.rate_limit.preset,
    requestsPerMinute: String(protection.rate_limit.requests_per_minute || 120),
    allowedIPs: protection.ip_access.allowed.join("\n"),
    blockedIPs: protection.ip_access.blocked.join("\n"),
    autoBanEnabled: protection.auto_ban.enabled,
    autoBanBlockedRequests: String(protection.auto_ban.blocked_requests || 20),
    autoBanWindowMinutes: String(protection.auto_ban.window_minutes || 10),
    autoBanMinutes: String(protection.auto_ban.ban_minutes || 60),
  };
}

function protectionFromForm(form: SecurityForm): ProtectionConfig {
  return {
    waf: {
      mode: form.wafMode,
      paranoia_level: parseInteger(form.paranoiaLevel, 1),
      excluded_rule_ids: parseRuleIDs(form.excludedRuleIDs),
      path_exclusions: parsePathExclusions(
        form.disabledPaths,
        form.pathRuleExclusions,
      ),
      custom_rules: form.customRules.trim(),
    },
    rate_limit: {
      enabled: form.rateLimitEnabled,
      preset: form.rateLimitPreset,
      requests_per_minute: parseInteger(form.requestsPerMinute, 120),
    },
    ip_access: {
      allowed: lineList(form.allowedIPs),
      blocked: lineList(form.blockedIPs),
    },
    auto_ban: {
      enabled: form.autoBanEnabled,
      blocked_requests: parseInteger(form.autoBanBlockedRequests, 20),
      window_minutes: parseInteger(form.autoBanWindowMinutes, 10),
      ban_minutes: parseInteger(form.autoBanMinutes, 60),
    },
  };
}

function parsePathExclusions(
  disabledPathsRaw: string,
  pathRuleExclusionsRaw: string,
): WAFPathExclusion[] {
  const disabled = lineList(disabledPathsRaw).map((path) => ({
    path,
    disable_waf: true,
    excluded_rule_ids: [],
  }));
  const ruleExclusions = lineList(pathRuleExclusionsRaw)
    .map((line) => {
      const [path = "", ...rest] = line.split(/\s+/);
      return {
        path,
        disable_waf: false,
        excluded_rule_ids: parseRuleIDs(rest.join(" ")),
      };
    })
    .filter((exclusion) => exclusion.path && exclusion.excluded_rule_ids.length > 0);

  return [...disabled, ...ruleExclusions];
}

function parseRuleIDs(value: string) {
  return Array.from(
    new Set(
      value
        .split(/[,\s]+/)
        .map((item) => Number.parseInt(item, 10))
        .filter((item) => Number.isFinite(item) && item > 0),
    ),
  ).sort((left, right) => left - right);
}

function parseInteger(value: string, fallback: number) {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function lineList(value: string) {
  return value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function validateSecurityForm(form: SecurityForm) {
  if (form.wafMode !== "disabled") {
    if (!validRuleIDList(form.excludedRuleIDs)) {
      return "Excluded rule IDs must be positive numbers separated by commas or spaces.";
    }
    for (const path of lineList(form.disabledPaths)) {
      if (!path.startsWith("/")) {
        return `Disabled path "${path}" must start with /.`;
      }
    }
    for (const line of lineList(form.pathRuleExclusions)) {
      const [path = "", ...ruleIDs] = line.split(/\s+/);
      if (
        !path.startsWith("/") ||
        ruleIDs.length === 0 ||
        !validRuleIDList(ruleIDs.join(" "), false)
      ) {
        return `Path rule exclusion "${line}" must use: /path 942100,941100`;
      }
    }
  }

  if (
    form.rateLimitEnabled &&
    !integerInRange(form.requestsPerMinute, 1, 10000)
  ) {
    return "Requests per minute must be a whole number between 1 and 10000.";
  }
  if (form.autoBanEnabled) {
    if (form.wafMode !== "blocking" && !form.rateLimitEnabled) {
      return "Auto-ban requires blocking WAF or rate limiting.";
    }
    if (!integerInRange(form.autoBanBlockedRequests, 1, 10000)) {
      return "Blocked requests must be a whole number between 1 and 10000.";
    }
    if (!integerInRange(form.autoBanWindowMinutes, 1, 1440)) {
      return "Window minutes must be a whole number between 1 and 1440.";
    }
    if (!integerInRange(form.autoBanMinutes, 1, 10080)) {
      return "Ban minutes must be a whole number between 1 and 10080.";
    }
  }
  return null;
}

function validRuleIDList(value: string, allowEmpty = true) {
  const trimmed = value.trim();
  return trimmed === ""
    ? allowEmpty
    : trimmed.split(/[,\s]+/).every((item) => /^[1-9]\d*$/.test(item));
}

function integerInRange(value: string, min: number, max: number) {
  return /^\d+$/.test(value) && Number(value) >= min && Number(value) <= max;
}

function firstFieldError(error: unknown) {
  if (typeof error !== "object" || error === null || !("fieldErrors" in error)) {
    return null;
  }
  const fieldErrors = (error as {
    fieldErrors?: Record<string, string>;
  }).fieldErrors;
  return fieldErrors ? Object.values(fieldErrors)[0] ?? null : null;
}
