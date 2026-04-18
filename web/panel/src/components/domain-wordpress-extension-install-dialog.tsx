import { useEffect, useMemo, useRef, useState } from "react";
import {
  installDomainWordPressExtension,
  searchDomainWordPressExtensions,
  type WordPressApiError,
  type WordPressExtension,
  type WordPressExtensionSearchResult,
  type WordPressStatus,
} from "@/api/domain-wordpress";
import {
  BrandWordpress,
  LoaderCircle,
  Plus,
  Search,
} from "@/components/icons/tabler-icons";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

type WordPressExtensionInstallDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hostname: string;
  type: "plugin" | "theme";
  installedItems: WordPressExtension[];
  onInstalled: (status: WordPressStatus) => void;
};

function WordPressExtensionSearchThumbnail({
  item,
}: {
  item: WordPressExtensionSearchResult;
}) {
  const [imageFailed, setImageFailed] = useState(false);
  const showImage = Boolean(item.thumbnail_url) && !imageFailed;

  return (
    <div className="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-md border border-[var(--app-border)] bg-[var(--app-surface)]">
      {showImage ? (
        <img
          src={item.thumbnail_url}
          alt=""
          className="h-full w-full object-cover"
          loading="lazy"
          onError={() => {
            setImageFailed(true);
          }}
        />
      ) : (
        <BrandWordpress
          className="h-5 w-5 text-[var(--app-text-muted)]"
          stroke={1.8}
        />
      )}
    </div>
  );
}

export function DomainWordPressExtensionInstallDialog({
  open,
  onOpenChange,
  hostname,
  type,
  installedItems,
  onInstalled,
}: WordPressExtensionInstallDialogProps) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<WordPressExtensionSearchResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [installingSlug, setInstallingSlug] = useState<string | null>(null);
  const requestIDRef = useRef(0);
  const typeLabel = type === "plugin" ? "Plugin" : "Theme";
  const installedSlugs = useMemo(
    () => new Set(installedItems.map((item) => item.name)),
    [installedItems],
  );
  const trimmedQuery = query.trim();

  useEffect(() => {
    requestIDRef.current += 1;
    setQuery("");
    setResults([]);
    setLoading(false);
    setError(null);
    setInstallingSlug(null);
  }, [hostname, open, type]);

  useEffect(() => {
    if (!open) {
      return;
    }

    if (trimmedQuery.length === 0) {
      setResults([]);
      setLoading(false);
      setError(null);
      return;
    }

    if (trimmedQuery.length < 2) {
      setResults([]);
      setLoading(false);
      setError(null);
      return;
    }

    const requestID = requestIDRef.current + 1;
    requestIDRef.current = requestID;
    setLoading(true);
    setError(null);

    const timeoutID = window.setTimeout(async () => {
      try {
        const nextResults = await searchDomainWordPressExtensions(
          hostname,
          type,
          trimmedQuery,
        );
        if (requestIDRef.current !== requestID) {
          return;
        }

        setResults(nextResults);
      } catch (searchError) {
        if (requestIDRef.current !== requestID) {
          return;
        }

        setResults([]);
        setError(
          getErrorMessage(
            searchError,
            `Failed to search WordPress ${type}s.`,
          ),
        );
      } finally {
        if (requestIDRef.current === requestID) {
          setLoading(false);
        }
      }
    }, 250);

    return () => {
      window.clearTimeout(timeoutID);
    };
  }, [hostname, open, trimmedQuery, type]);

  async function handleInstall(slug: string) {
    if (installingSlug || installedSlugs.has(slug)) {
      return;
    }

    setInstallingSlug(slug);

    try {
      const nextStatus = await installDomainWordPressExtension(hostname, type, {
        slug,
      });
      onInstalled(nextStatus);
      toast.success(`${typeLabel} ${slug} installed.`);
    } catch (installError) {
      const wordPressError = installError as WordPressApiError;
      const message =
        wordPressError.fieldErrors?.slug ||
        getErrorMessage(
          installError,
          `Failed to install WordPress ${type} ${slug}.`,
        );
      setError(message);
      toast.error(message);
    } finally {
      setInstallingSlug(null);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-4 sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {hostname} Add New {typeLabel}
          </DialogTitle>
          <DialogDescription>
            Search the WordPress directory and install a {type} into this site.
          </DialogDescription>
        </DialogHeader>

        <section className="space-y-3">
          <label
            htmlFor={`wordpress-${type}-search`}
            className="text-sm font-medium text-[var(--app-text)]"
          >
            Search {typeLabel}s
          </label>
          <div className="relative">
            <Search
              className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-[var(--app-text-muted)]"
              stroke={1.8}
            />
            <Input
              id={`wordpress-${type}-search`}
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
              }}
              placeholder={`Search WordPress ${type}s`}
              className="pl-9"
              autoComplete="off"
            />
          </div>
        </section>

        {error ? (
          <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
            {error}
          </div>
        ) : null}

        <section className="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
          <div className="flex items-center gap-2 border-b border-[var(--app-border)] px-4 py-3 text-sm text-[var(--app-text-muted)]">
            <BrandWordpress className="h-4 w-4" stroke={1.8} />
            <span>{typeLabel} directory</span>
          </div>

          <div className="max-h-[420px] overflow-y-auto">
            {loading ? (
              <div className="flex items-center gap-2 px-4 py-5 text-sm text-[var(--app-text-muted)]">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Searching {type}s...
              </div>
            ) : trimmedQuery.length < 2 ? (
              <div className="px-4 py-5 text-sm text-[var(--app-text-muted)]">
                Enter at least 2 characters to search the WordPress directory.
              </div>
            ) : results.length === 0 ? (
              <div className="px-4 py-5 text-sm text-[var(--app-text-muted)]">
                No {type}s matched "{trimmedQuery}".
              </div>
            ) : (
              <div className="divide-y divide-[var(--app-border)]">
                {results.map((item) => {
                  const installed = installedSlugs.has(item.slug);
                  const busy = installingSlug === item.slug;

                  return (
                    <div
                      key={item.slug}
                      className="grid gap-3 px-4 py-4 md:grid-cols-[56px_minmax(0,1fr)_auto]"
                    >
                      <WordPressExtensionSearchThumbnail item={item} />
                      <div className="min-w-0 space-y-1">
                        <div className="flex min-w-0 items-center gap-2">
                          <div
                            className="truncate text-sm font-medium text-[var(--app-text)]"
                            title={item.name || item.slug}
                          >
                            {item.name || item.slug}
                          </div>
                          {installed ? (
                            <span className="rounded-md border border-[var(--app-border)] bg-[var(--app-surface)] px-2 py-0.5 text-[11px] font-medium text-[var(--app-text-muted)]">
                              Installed
                            </span>
                          ) : null}
                        </div>
                        <div className="font-mono text-[12px] text-[var(--app-text-muted)]">
                          {item.slug}
                          {item.version ? ` · v${item.version}` : ""}
                          {item.author ? ` · ${item.author}` : ""}
                        </div>
                        {item.last_updated ? (
                          <div className="text-[12px] text-[var(--app-text-muted)]">
                            Updated {item.last_updated}
                          </div>
                        ) : null}
                      </div>
                      <div className="flex items-start md:justify-end">
                        <Button
                          type="button"
                          size="sm"
                          disabled={installed || installingSlug !== null}
                          onClick={() => {
                            void handleInstall(item.slug);
                          }}
                        >
                          {busy ? (
                            <>
                              <LoaderCircle className="h-4 w-4 animate-spin" />
                              Installing...
                            </>
                          ) : installed ? (
                            "Installed"
                          ) : (
                            <>
                              <Plus className="h-4 w-4" stroke={1.8} />
                              Install
                            </>
                          )}
                        </Button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </section>
      </DialogContent>
    </Dialog>
  );
}
