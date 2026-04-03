import { useParams } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { fetchDomains, type DomainRecord } from "@/api/domains";
import { PageHeader } from "@/components/page-header";

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
        <div>
          {loadError ? (
            <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
              {loadError}
            </section>
          ) : null}
        </div>
      </div>
    </>
  );
}
