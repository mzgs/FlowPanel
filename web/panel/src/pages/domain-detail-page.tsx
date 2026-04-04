import { useNavigate, useParams } from "@tanstack/react-router";
import { useEffect, useState, type ComponentType } from "react";
import {
  fetchDomainPreview,
  fetchDomains,
  getDomainSiteUrl,
  type DomainRecord,
} from "@/api/domains";
import {
  Clock,
  Copy,
  Database,
  Download,
  ExternalLink,
  File,
  FileCode2,
  Folder,
  FolderOpen,
  GitBranch,
  Globe,
  HardDrive,
  LoaderCircle,
  Monitor,
  Package,
  RefreshCw,
  Settings2,
  Sparkles,
  Telescope,
  TerminalSquare,
} from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { getFilesPathFromDomainTarget } from "@/lib/domain-targets";
import { cn } from "@/lib/utils";

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

type ActionIcon = ComponentType<{
  className?: string;
  size?: number | string;
  stroke?: number | string;
}>;

type DomainActionItem = {
  title: string;
  icon: ActionIcon;
};

const fileAndDatabaseActions: DomainActionItem[] = [
  {
    title: "Connection Info",
    icon: Globe,
  },
  {
    title: "Files",
    icon: Folder,
  },
  {
    title: "Databases",
    icon: Database,
  },
  {
    title: "FTP",
    icon: FolderOpen,
  },
  {
    title: "Backup & Restore",
    icon: HardDrive,
  },
  {
    title: "Website Copying",
    icon: Copy,
  },
];

const devToolActions: DomainActionItem[] = [
  {
    title: "PHP",
    icon: FileCode2,
  },
  {
    title: "Logs",
    icon: File,
  },
  {
    title: "SSH Terminal",
    icon: TerminalSquare,
  },
  {
    title: "Monitoring",
    icon: Monitor,
  },
  {
    title: "PHP Composer",
    icon: Package,
  },
  {
    title: "Scheduled Tasks",
    icon: Clock,
  },
  {
    title: "Performance Booster",
    icon: Sparkles,
  },
  {
    title: "Git",
    icon: GitBranch,
  },
  {
    title: "SEO",
    icon: Telescope,
  },
  {
    title: "Website Importing",
    icon: Download,
  },
  {
    title: "Docker Proxy Rules",
    icon: Settings2,
  },
];

function DomainActionSection({
  title,
  items,
  onItemClick,
}: {
  title: string;
  items: DomainActionItem[];
  onItemClick?: (item: DomainActionItem) => void;
}) {
  return (
    <section className="space-y-2">
      <h2 className="text-base font-semibold text-[var(--app-text)]">{title}</h2>
      <div className="grid gap-x-3 gap-y-1.5 md:grid-cols-2 xl:grid-cols-3">
        {items.map(({ title: itemTitle, icon: Icon }) => (
          <button
            key={itemTitle}
            type="button"
            onClick={() => onItemClick?.({ title: itemTitle, icon: Icon })}
            className="group flex items-center gap-3 rounded-lg px-2 py-1 text-left transition-colors duration-150 hover:bg-[var(--app-surface-muted)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--app-accent)]"
          >
            <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] text-[var(--app-text-muted)] transition-colors duration-150 group-hover:text-[var(--app-accent)]">
              <Icon className="h-5 w-5" stroke={1.75} />
            </span>
            <span className="min-w-0 text-sm font-medium leading-5 text-[var(--app-text)]">
              {itemTitle}
            </span>
          </button>
        ))}
      </div>
    </section>
  );
}

export function DomainDetailPage() {
  const { hostname } = useParams({ from: "/domains/$hostname" });
  const navigate = useNavigate();
  const [domain, setDomain] = useState<DomainRecord | null>(null);
  const [sitesBasePath, setSitesBasePath] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [previewUrl, setPreviewUrl] = useState("");
  const [previewLoaded, setPreviewLoaded] = useState(false);
  const [previewError, setPreviewError] = useState(false);
  const [previewErrorMessage, setPreviewErrorMessage] = useState<string | null>(null);
  const [previewRefreshing, setPreviewRefreshing] = useState(false);
  const [previewRefreshToken, setPreviewRefreshToken] = useState(0);
  const siteUrl = domain ? getDomainSiteUrl(domain.hostname) : "";

  useEffect(() => {
    let active = true;
    setLoading(true);
    setLoadError(null);
    setDomain(null);
    setSitesBasePath("");
    setPreviewUrl("");
    setPreviewLoaded(false);
    setPreviewError(false);
    setPreviewErrorMessage(null);
    setPreviewRefreshing(false);
    setPreviewRefreshToken(0);

    async function loadDomain() {
      try {
        const payload = await fetchDomains();
        if (!active) {
          return;
        }

        const matchedDomain =
          payload.domains.find((record) => record.hostname === hostname) ?? null;

        setSitesBasePath(payload.sites_base_path);
        setDomain(matchedDomain);
        setLoadError(matchedDomain ? null : "The selected domain could not be found.");
      } catch (error) {
        if (!active) {
          return;
        }

        setLoadError(getErrorMessage(error, "Failed to load domain details."));
      } finally {
        if (active) {
          setLoading(false);
        }
      }
    }

    void loadDomain();

    return () => {
      active = false;
    };
  }, [hostname]);

  useEffect(() => {
    if (!previewUrl.startsWith("blob:")) {
      return;
    }

    return () => {
      URL.revokeObjectURL(previewUrl);
    };
  }, [previewUrl]);

  useEffect(() => {
    if (!domain) {
      return;
    }

    let active = true;
    const controller = new AbortController();
    const refreshRequested = previewRefreshToken > 0;

    if (!previewUrl) {
      setPreviewLoaded(false);
    }
    setPreviewError(false);
    setPreviewErrorMessage(null);
    setPreviewRefreshing(refreshRequested);

    async function loadPreview() {
      try {
        const blob = await fetchDomainPreview(domain.hostname, {
          refresh: refreshRequested,
          refreshToken: previewRefreshToken || undefined,
          signal: controller.signal,
        });
        if (!active) {
          return;
        }

        const objectUrl = URL.createObjectURL(blob);
        setPreviewUrl((currentUrl) => {
          if (currentUrl.startsWith("blob:")) {
            URL.revokeObjectURL(currentUrl);
          }

          return objectUrl;
        });
      } catch (error) {
        if (!active) {
          return;
        }

        setPreviewLoaded(false);
        setPreviewError(!previewUrl);
        setPreviewErrorMessage(getErrorMessage(error, "Preview is unavailable right now."));
        setPreviewRefreshing(false);
      }
    }

    void loadPreview();

    return () => {
      active = false;
      controller.abort();
    };
  }, [domain?.hostname, previewRefreshToken]);

  const filesPath = domain
    ? getFilesPathFromDomainTarget(domain.kind, sitesBasePath, domain.target)
    : null;

  return (
    <>
      <PageHeader
        title={
          loading ? (
            "Domain details"
          ) : domain ? (
            <span className="flex flex-wrap items-center gap-3">
              <span>{domain.hostname}</span>
              <Badge asChild variant="outline" className="rounded-full align-middle">
                <a
                  href={siteUrl}
                  target="_blank"
                  rel="noreferrer"
                  aria-label={`Visit ${domain.hostname}`}
                  title={`Visit ${domain.hostname}`}
                >
                  <ExternalLink className="h-3 w-3" />
                  Visit
                </a>
              </Badge>
            </span>
          ) : (
            "Domain details"
          )
        }
        meta={
          loading
            ? "Loading domain details..."
            : domain
              ? "Files, databases, and developer tools for this domain."
              : "This route is reserved for per-domain configuration."
        }
      />

      <div className="px-4 pb-1 sm:px-6 lg:px-8">
        <div className="space-y-4">
          {loadError ? (
            <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
              {loadError}
            </section>
          ) : null}

          {!loadError ? (
            <section className="grid gap-4 xl:grid-cols-[280px_minmax(0,1fr)]">
              <aside className="space-y-3">
                <div className="w-[280px] max-w-full overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] shadow-[var(--app-shadow)]">
                  <div className="relative aspect-[4/3] w-full bg-[var(--app-surface-muted)]">
                    {domain ? (
                      <>
                        <a
                          href={siteUrl}
                          target="_blank"
                          rel="noreferrer"
                          aria-label={`Visit ${domain.hostname}`}
                          title={`Visit ${domain.hostname}`}
                          className="group block h-full w-full"
                        >
                          {previewUrl ? (
                            <img
                              src={previewUrl}
                              alt={`${domain.hostname} site preview`}
                              className={cn(
                                "h-full w-full object-contain transition-opacity duration-200",
                                previewLoaded ? "opacity-100" : "opacity-0",
                              )}
                              loading="eager"
                              onLoad={() => {
                                setPreviewLoaded(true);
                                setPreviewError(false);
                                setPreviewErrorMessage(null);
                                setPreviewRefreshing(false);
                              }}
                              onError={() => {
                                setPreviewLoaded(false);
                                setPreviewError(true);
                                setPreviewErrorMessage("Preview image could not be displayed.");
                                setPreviewRefreshing(false);
                              }}
                            />
                          ) : null}

                          {!previewLoaded && (!previewUrl || previewError) ? (
                            <div className="absolute inset-0 flex flex-col justify-between bg-[var(--app-surface)]/92 p-4">
                              <div className="inline-flex w-fit rounded-full border border-[var(--app-border)] bg-[var(--app-surface)]/85 px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--app-text-muted)]">
                                Preview
                              </div>
                              <div>
                                <p className="text-sm font-semibold text-[var(--app-text)]">{domain.hostname}</p>
                                <p className="mt-1 text-xs text-[var(--app-text-muted)]">
                                  {previewError
                                    ? previewErrorMessage ?? "Preview is unavailable right now."
                                    : previewRefreshing
                                      ? "Refreshing preview..."
                                      : "Loading cached preview..."}
                                </p>
                              </div>
                            </div>
                          ) : null}
                        </a>

                        <button
                          type="button"
                          className="absolute right-3 bottom-3 z-10 inline-flex h-9 w-9 items-center justify-center rounded-full border border-[var(--app-border)] bg-[var(--app-surface)]/92 text-[var(--app-text)] shadow-[var(--app-shadow)] transition hover:bg-[var(--app-surface)] disabled:cursor-not-allowed disabled:opacity-70"
                          aria-label={`Refresh preview for ${domain.hostname}`}
                          title="Refresh preview"
                          disabled={previewRefreshing}
                          onClick={() => {
                            setPreviewRefreshing(true);
                            setPreviewError(false);
                            setPreviewErrorMessage(null);
                            setPreviewRefreshToken(Date.now());
                          }}
                        >
                          {previewRefreshing ? (
                            <LoaderCircle className="h-4 w-4 animate-spin" />
                          ) : (
                            <RefreshCw className="h-4 w-4" />
                          )}
                        </button>
                      </>
                    ) : (
                      <div className="flex h-full items-center justify-center text-sm text-[var(--app-text-muted)]">
                        Loading preview...
                      </div>
                    )}
                  </div>
                </div>
                {previewErrorMessage && previewUrl ? (
                  <p className="text-xs leading-5 text-[var(--app-text-muted)]">
                    {previewErrorMessage}
                  </p>
                ) : null}

                <section className="w-[280px] max-w-full rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] p-5 shadow-[var(--app-shadow)]">
                  <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--app-text-muted)]">
                    Overview
                  </p>
                  <dl className="mt-4 space-y-4 text-sm">
                    <div>
                      <dt className="text-[var(--app-text-muted)]">Hostname</dt>
                      <dd className="mt-1 font-medium text-[var(--app-text)]">{domain?.hostname ?? "..."}</dd>
                    </div>
                    <div>
                      <dt className="text-[var(--app-text-muted)]">Type</dt>
                      <dd className="mt-1 font-medium text-[var(--app-text)]">{domain?.kind ?? "..."}</dd>
                    </div>
                    <div>
                      <dt className="text-[var(--app-text-muted)]">Caching</dt>
                      <dd className="mt-1 font-medium text-[var(--app-text)]">
                        {domain ? (domain.cache_enabled ? "Enabled" : "Disabled") : "..."}
                      </dd>
                    </div>
                  </dl>
                </section>
              </aside>
              <div className="space-y-4">
                <DomainActionSection
                  title="Files & Databases"
                  items={fileAndDatabaseActions}
                  onItemClick={(item) => {
                    if (item.title !== "Files" || filesPath === null) {
                      return;
                    }

                    void navigate({
                      to: "/files",
                      search: filesPath ? { path: filesPath } : {},
                    });
                  }}
                />
                <DomainActionSection title="Dev Tools" items={devToolActions} />
              </div>
            </section>
          ) : null}
        </div>
      </div>
    </>
  );
}
