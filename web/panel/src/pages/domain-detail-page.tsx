import { useParams } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { fetchDomainPreview, fetchDomains, type DomainRecord } from "@/api/domains";
import { LoaderCircle, RefreshCw } from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { cn } from "@/lib/utils";

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

export function DomainDetailPage() {
  const { hostname } = useParams({ from: "/domains/$hostname" });
  const [domain, setDomain] = useState<DomainRecord | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [previewUrl, setPreviewUrl] = useState("");
  const [previewLoaded, setPreviewLoaded] = useState(false);
  const [previewError, setPreviewError] = useState(false);
  const [previewErrorMessage, setPreviewErrorMessage] = useState<string | null>(null);
  const [previewRefreshing, setPreviewRefreshing] = useState(false);
  const [previewRefreshToken, setPreviewRefreshToken] = useState(0);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setLoadError(null);
    setDomain(null);
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

  return (
    <>
      <PageHeader
        title={loading ? "Domain details" : domain?.hostname ?? "Domain details"}
        meta={
          loading
            ? "Loading domain details..."
            : domain
              ? "Settings and site configuration will live here."
              : "This route is reserved for per-domain configuration."
        }
      />

      <div className="px-4 pb-6 sm:px-6 lg:px-8">
        <div className="space-y-6">
          {loadError ? (
            <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
              {loadError}
            </section>
          ) : null}

          {!loadError ? (
            <section className="grid gap-6 xl:grid-cols-[280px_minmax(0,1fr)]">
              <aside className="space-y-3">
                <div className="w-[280px] max-w-full overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] shadow-[var(--app-shadow)]">
                  <div className="relative aspect-[4/3] w-full bg-[var(--app-surface-muted)]">
                    {domain ? (
                      <>
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

                        <button
                          type="button"
                          className="absolute right-3 bottom-3 inline-flex h-9 w-9 items-center justify-center rounded-full border border-[var(--app-border)] bg-[var(--app-surface)]/92 text-[var(--app-text)] shadow-[var(--app-shadow)] transition hover:bg-[var(--app-surface)] disabled:cursor-not-allowed disabled:opacity-70"
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
              </aside>

              <div className="grid gap-4 md:grid-cols-2">
                <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] p-5 shadow-[var(--app-shadow)]">
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

                <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] p-5 shadow-[var(--app-shadow)]">
                  <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--app-text-muted)]">
                    Target
                  </p>
                  <p className="mt-4 break-all text-sm leading-6 text-[var(--app-text)]">
                    {domain?.target ?? "Loading target..."}
                  </p>
                  <p className="mt-4 text-xs leading-5 text-[var(--app-text-muted)]">
                    Site previews are cached on the server. Use refresh to fetch a new thumbnail.
                  </p>
                </section>
              </div>
            </section>
          ) : null}
        </div>
      </div>
    </>
  );
}
