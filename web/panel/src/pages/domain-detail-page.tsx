import { useParams } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { Settings } from "@/components/icons/tabler-icons";
import { fetchDomains, type DomainRecord } from "@/api/domains";
import { PageHeader } from "@/components/page-header";
import { formatDateTime } from "@/lib/format";

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

export function DomainDetailPage() {
  const { domainId } = useParams({ from: "/domains/$domainId" });
  const [domain, setDomain] = useState<DomainRecord | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setLoadError(null);
    setDomain(null);

    async function loadDomain() {
      try {
        const payload = await fetchDomains();
        if (!active) {
          return;
        }

        const matchedDomain =
          payload.domains.find((record) => record.id === domainId) ?? null;

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
  }, [domainId]);

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
        <div className="space-y-5">
          {loadError ? (
            <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
              {loadError}
            </section>
          ) : null}

          {!loading && domain ? (
            <>
              <section className="grid gap-4 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
                <div className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] p-5 shadow-[var(--app-shadow)]">
                  <div className="mb-4 text-[14px] font-medium text-[var(--app-text)]">
                    Overview
                  </div>
                  <dl className="grid gap-4 sm:grid-cols-2">
                    <div>
                      <dt className="text-[12px] uppercase tracking-wide text-[var(--app-text-muted)]">
                        Domain
                      </dt>
                      <dd className="mt-1 text-[14px] font-medium text-[var(--app-text)]">
                        {domain.hostname}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-[12px] uppercase tracking-wide text-[var(--app-text-muted)]">
                        Type
                      </dt>
                      <dd className="mt-1 text-[14px] text-[var(--app-text)]">{domain.kind}</dd>
                    </div>
                    <div>
                      <dt className="text-[12px] uppercase tracking-wide text-[var(--app-text-muted)]">
                        Target
                      </dt>
                      <dd className="mt-1 break-all font-mono text-[12px] text-[var(--app-text-muted)]">
                        {domain.target}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-[12px] uppercase tracking-wide text-[var(--app-text-muted)]">
                        Created
                      </dt>
                      <dd className="mt-1 text-[14px] text-[var(--app-text)]">
                        {formatDateTime(domain.created_at)}
                      </dd>
                    </div>
                  </dl>
                </div>

                <div className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] p-5 shadow-[var(--app-shadow)]">
                  <div className="mb-4 flex items-center gap-2 text-[14px] font-medium text-[var(--app-text)]">
                    <Settings className="h-4 w-4" />
                    Settings
                  </div>
                  <p className="text-[13px] leading-6 text-[var(--app-text-muted)]">
                    This area will hold per-domain settings such as routing behavior,
                    cache rules, and publishing controls.
                  </p>
                </div>
              </section>

              <section className="rounded-2xl border border-dashed border-[var(--app-border)] bg-[var(--app-bg-2)] p-5 shadow-[var(--app-shadow)]">
                <div className="mb-2 text-[14px] font-medium text-[var(--app-text)]">
                  Site config
                </div>
                <p className="text-[13px] leading-6 text-[var(--app-text-muted)]">
                  Reserved for site-specific configuration, deployment options, and future
                  domain management tools.
                </p>
              </section>
            </>
          ) : null}
        </div>
      </div>
    </>
  );
}
