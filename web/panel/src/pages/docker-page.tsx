import { useEffect, useRef, useState } from "react";
import {
  createDockerContainer,
  fetchDockerContainers,
  fetchDockerImages,
  fetchDockerStatus,
  type DockerContainer,
  type DockerHubImage,
  type DockerImage,
  type DockerStatus,
  searchDockerHubImages,
} from "@/api/docker";
import {
  AdjustmentsHorizontal,
  ChevronDownIcon,
  Docker,
  DotsVertical,
  LoaderCircle,
  Package,
  Plus,
  RefreshCw,
  Search,
} from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { cn, getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

type LoadOptions = {
  silent?: boolean;
};

type DockerTab = "containers" | "images";

function getContainerStateMeta(state: DockerContainer["state"]) {
  switch (state) {
    case "running":
      return { label: "Running", dotClassName: "bg-[var(--app-ok)]" };
    case "restarting":
      return { label: "Restarting", dotClassName: "bg-[var(--app-warning)]" };
    case "paused":
      return { label: "Paused", dotClassName: "bg-[var(--app-warning)]" };
    case "created":
      return { label: "Created", dotClassName: "bg-muted-foreground/60" };
    case "dead":
      return { label: "Dead", dotClassName: "bg-[var(--app-danger)]" };
    case "exited":
      return { label: "Exited", dotClassName: "bg-muted-foreground/60" };
    default:
      return { label: "Unknown", dotClassName: "bg-muted-foreground/60" };
  }
}

function getPageMeta(
  status: DockerStatus | null,
  containers: DockerContainer[],
  images: DockerImage[],
  activeTab: DockerTab,
) {
  if (!status) {
    if (activeTab === "containers" && containers.length > 0) {
      return `${containers.length} containers found on this node.`;
    }
    if (activeTab === "images" && images.length > 0) {
      return `${images.length} Docker images found on this node.`;
    }
    return activeTab === "containers"
      ? "Container inventory for this node."
      : "Docker image inventory for this node.";
  }

  if (!status.installed) {
    return "Docker is not installed on this node yet.";
  }

  if (!status.service_running) {
    return "Docker is installed, but the daemon is not running.";
  }

  if (activeTab === "containers") {
    const runningCount = containers.filter((container) => container.state === "running").length;
    if (containers.length === 0) {
      return "Docker is running. No containers were found.";
    }
    return `${containers.length} containers found, ${runningCount} running.`;
  }

  if (images.length === 0) {
    return "Docker is running. No images were found.";
  }

  return `${images.length} Docker images found on this node.`;
}

function getContainerLabel(container: DockerContainer) {
  const trimmedName = container.name.trim();
  if (trimmedName) {
    return trimmedName;
  }

  return container.id.slice(0, 12);
}

function ContainersSkeleton() {
  return (
    <div className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
      <div className="hidden grid-cols-[minmax(0,1.1fr)_minmax(0,1.2fr)_minmax(140px,0.55fr)_72px] gap-6 border-b border-[var(--app-border)] px-6 py-4 text-sm text-muted-foreground md:grid">
        <div>Name</div>
        <div>Image</div>
        <div>Status</div>
        <div />
      </div>
      {Array.from({ length: 4 }).map((_, index) => (
        <div
          key={index}
          className="grid gap-4 border-b border-[var(--app-border)] px-4 py-4 last:border-b-0 md:grid-cols-[minmax(0,1.1fr)_minmax(0,1.2fr)_minmax(140px,0.55fr)_72px] md:px-6"
        >
          <div className="h-5 w-40 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="h-5 w-52 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="h-5 w-24 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="hidden h-5 w-12 animate-pulse rounded bg-[var(--app-surface)] md:block" />
        </div>
      ))}
    </div>
  );
}

function ImagesSkeleton() {
  return (
    <div className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
      <div className="hidden grid-cols-[minmax(0,1.2fr)_160px_140px_140px_72px] gap-6 border-b border-[var(--app-border)] px-6 py-4 text-sm text-muted-foreground md:grid">
        <div>Repository</div>
        <div>Tag</div>
        <div>Size</div>
        <div>Created</div>
        <div />
      </div>
      {Array.from({ length: 4 }).map((_, index) => (
        <div
          key={index}
          className="grid gap-4 border-b border-[var(--app-border)] px-4 py-4 last:border-b-0 md:grid-cols-[minmax(0,1.2fr)_160px_140px_140px_72px] md:px-6"
        >
          <div className="h-5 w-44 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="h-5 w-20 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="h-5 w-16 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="h-5 w-24 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="hidden h-5 w-12 animate-pulse rounded bg-[var(--app-surface)] md:block" />
        </div>
      ))}
    </div>
  );
}

type EmptyStateProps = {
  title: string;
  description: string;
};

function DockerEmptyState({ title, description }: EmptyStateProps) {
  return (
    <div className="flex min-h-[320px] items-center justify-center rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-6 py-10 text-center shadow-[var(--app-shadow)]">
      <div className="max-w-md space-y-4">
        <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)]">
          <Docker className="h-7 w-7" />
        </div>
        <div className="space-y-2">
          <h2 className="text-xl font-semibold tracking-tight text-foreground">{title}</h2>
          <p className="text-sm leading-6 text-muted-foreground">{description}</p>
        </div>
      </div>
    </div>
  );
}

function TabButton({
  active,
  label,
  count,
  onClick,
}: {
  active: boolean;
  label: string;
  count: number;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "flex min-w-0 flex-1 items-center justify-between gap-3 rounded-xl px-4 py-3 text-left transition-colors",
        active
          ? "bg-background text-foreground shadow-[var(--app-shadow)]"
          : "text-muted-foreground hover:bg-[var(--app-surface)] hover:text-foreground",
      )}
    >
      <span className="truncate text-sm font-medium">{label}</span>
      <span
        className={cn(
          "rounded-full px-2 py-0.5 text-xs font-medium",
          active ? "bg-[var(--app-surface)] text-foreground" : "bg-[var(--app-surface)]/70",
        )}
      >
        {count}
      </span>
    </button>
  );
}

function ContainerList({ containers }: { containers: DockerContainer[] }) {
  return (
    <div className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
      <div className="hidden grid-cols-[minmax(0,1.1fr)_minmax(0,1.2fr)_minmax(140px,0.55fr)_72px] items-center gap-6 border-b border-[var(--app-border)] px-6 py-5 text-sm text-muted-foreground md:grid">
        <div className="flex items-center gap-3">
          <ChevronDownIcon className="h-4 w-4 text-muted-foreground/70" />
          <span>Name</span>
        </div>
        <div>Image ↑</div>
        <div>Status</div>
        <div />
      </div>

      {containers.map((container) => {
        const stateMeta = getContainerStateMeta(container.state);

        return (
          <div
            key={container.id || `${container.name}-${container.image}`}
            className="grid gap-4 border-b border-[var(--app-border)] px-4 py-4 last:border-b-0 md:grid-cols-[minmax(0,1.1fr)_minmax(0,1.2fr)_minmax(140px,0.55fr)_72px] md:px-6 md:py-5"
          >
            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Name
              </div>
              <div className="flex min-w-0 items-center gap-3">
                <ChevronDownIcon className="h-4 w-4 shrink-0 text-muted-foreground/70" />
                <div className="truncate text-[15px] font-medium text-foreground">{container.name}</div>
              </div>
            </div>

            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Image
              </div>
              <div className="flex min-w-0 items-center gap-2.5 text-[15px] text-foreground">
                <Docker className="h-4 w-4 shrink-0 text-muted-foreground" />
                <span className="truncate">{container.image}</span>
              </div>
            </div>

            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Status
              </div>
              <div className="inline-flex items-center gap-2 text-[15px] font-medium text-foreground" title={container.status}>
                <span className={`h-2.5 w-2.5 rounded-full ${stateMeta.dotClassName}`} />
                <span>{stateMeta.label}</span>
              </div>
            </div>

            <div className="hidden items-center justify-end gap-1 text-muted-foreground/70 md:flex">
              <span className="rounded-full p-2">
                <AdjustmentsHorizontal className="h-4 w-4" />
              </span>
              <span className="rounded-full p-2">
                <DotsVertical className="h-4 w-4" />
              </span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function ImageList({ images }: { images: DockerImage[] }) {
  return (
    <div className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
      <div className="hidden grid-cols-[minmax(0,1.2fr)_160px_140px_140px_72px] items-center gap-6 border-b border-[var(--app-border)] px-6 py-5 text-sm text-muted-foreground md:grid">
        <div className="flex items-center gap-3">
          <Package className="h-4 w-4 text-muted-foreground/70" />
          <span>Repository</span>
        </div>
        <div>Tag</div>
        <div>Size</div>
        <div>Created</div>
        <div />
      </div>

      {images.map((image) => {
        const repository = image.repository || "<none>";
        const tag = image.tag || "<none>";

        return (
          <div
            key={`${image.id}-${repository}-${tag}`}
            className="grid gap-4 border-b border-[var(--app-border)] px-4 py-4 last:border-b-0 md:grid-cols-[minmax(0,1.2fr)_160px_140px_140px_72px] md:px-6 md:py-5"
          >
            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Repository
              </div>
              <div className="flex min-w-0 items-center gap-2.5 text-[15px] text-foreground">
                <Package className="h-4 w-4 shrink-0 text-muted-foreground" />
                <span className="truncate">{repository}</span>
              </div>
            </div>

            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Tag
              </div>
              <div className="truncate text-[15px] text-foreground">{tag}</div>
            </div>

            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Size
              </div>
              <div className="truncate text-[15px] text-foreground">{image.size || "—"}</div>
            </div>

            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Created
              </div>
              <div className="truncate text-[15px] text-foreground">{image.created_since || "—"}</div>
            </div>

            <div className="hidden items-center justify-end gap-1 text-muted-foreground/70 md:flex">
              <span className="rounded-full p-2">
                <AdjustmentsHorizontal className="h-4 w-4" />
              </span>
              <span className="rounded-full p-2">
                <DotsVertical className="h-4 w-4" />
              </span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

type AddDockerContainerDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (container: DockerContainer) => void;
};

function AddDockerContainerDialog({
  open,
  onOpenChange,
  onCreated,
}: AddDockerContainerDialogProps) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<DockerHubImage[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [creatingImage, setCreatingImage] = useState<string | null>(null);
  const requestIDRef = useRef(0);
  const trimmedQuery = query.trim();

  useEffect(() => {
    requestIDRef.current += 1;
    setQuery("");
    setResults([]);
    setLoading(false);
    setError(null);
    setCreatingImage(null);
  }, [open]);

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
        const nextResults = await searchDockerHubImages(trimmedQuery);
        if (requestIDRef.current !== requestID) {
          return;
        }

        setResults(nextResults);
      } catch (searchError) {
        if (requestIDRef.current !== requestID) {
          return;
        }

        setResults([]);
        setError(getErrorMessage(searchError, "Failed to search Docker Hub."));
      } finally {
        if (requestIDRef.current === requestID) {
          setLoading(false);
        }
      }
    }, 250);

    return () => {
      window.clearTimeout(timeoutID);
    };
  }, [open, trimmedQuery]);

  async function handleCreate(image: string) {
    if (creatingImage !== null) {
      return;
    }

    setCreatingImage(image);
    setError(null);

    try {
      const container = await createDockerContainer({ image });
      onCreated(container);
      onOpenChange(false);
    } catch (createError) {
      const message = getErrorMessage(createError, `Failed to create a container from ${image}.`);
      setError(message);
      toast.error(message);
    } finally {
      setCreatingImage(null);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && creatingImage !== null) {
          return;
        }
        onOpenChange(nextOpen);
      }}
    >
      <DialogContent className="gap-4 sm:max-w-3xl" showCloseButton={creatingImage === null}>
        <DialogHeader>
          <DialogTitle>Add Container</DialogTitle>
          <DialogDescription>
            Search Docker Hub and create a stopped container from a selected image. FlowPanel pulls the
            image first if it is missing locally.
          </DialogDescription>
        </DialogHeader>

        <section className="space-y-3">
          <label htmlFor="docker-image-search" className="text-sm font-medium text-foreground">
            Search Docker Hub images
          </label>
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="docker-image-search"
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
              }}
              placeholder="Search images like nginx, redis, postgres..."
              className="pl-9"
              autoComplete="off"
              disabled={creatingImage !== null}
            />
          </div>
        </section>

        {error ? (
          <div className="rounded-xl border border-[var(--app-danger-soft)] bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-foreground">
            {error}
          </div>
        ) : null}

        <section className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
          <div className="flex items-center gap-2 border-b border-[var(--app-border)] px-4 py-3 text-sm text-muted-foreground">
            <Docker className="h-4 w-4" />
            <span>Docker Hub results</span>
          </div>

          <div className="max-h-[420px] overflow-y-auto">
            {loading ? (
              <div className="flex items-center gap-2 px-4 py-5 text-sm text-muted-foreground">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Searching Docker Hub...
              </div>
            ) : trimmedQuery.length < 2 ? (
              <div className="px-4 py-5 text-sm text-muted-foreground">
                Enter at least 2 characters to search Docker Hub.
              </div>
            ) : results.length === 0 ? (
              <div className="px-4 py-5 text-sm text-muted-foreground">
                No images matched "{trimmedQuery}".
              </div>
            ) : (
              <div className="divide-y divide-[var(--app-border)]">
                {results.map((result) => {
                  const busy = creatingImage === result.name;

                  return (
                    <div
                      key={result.name}
                      className="grid gap-3 px-4 py-4 md:grid-cols-[minmax(0,1fr)_auto]"
                    >
                      <div className="min-w-0 space-y-2">
                        <div className="flex min-w-0 flex-wrap items-center gap-2">
                          <div className="truncate text-sm font-medium text-foreground" title={result.name}>
                            {result.name}
                          </div>
                          {result.is_official ? (
                            <span className="rounded-full border border-[var(--app-border)] bg-[var(--app-surface)] px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
                              Official
                            </span>
                          ) : null}
                          <span className="text-xs text-muted-foreground">
                            {result.star_count.toLocaleString()} stars
                          </span>
                        </div>
                        <p className="text-sm leading-6 text-muted-foreground">
                          {result.description || "No Docker Hub description was provided for this image."}
                        </p>
                        <div className="font-mono text-[12px] text-muted-foreground">
                          docker pull {result.name}
                        </div>
                      </div>

                      <div className="flex items-start md:justify-end">
                        <Button
                          type="button"
                          size="sm"
                          disabled={creatingImage !== null}
                          onClick={() => {
                            void handleCreate(result.name);
                          }}
                        >
                          {busy ? (
                            <>
                              <LoaderCircle className="h-4 w-4 animate-spin" />
                              Creating...
                            </>
                          ) : (
                            <>
                              <Plus className="h-4 w-4" />
                              Create Container
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

export function DockerPage() {
  const [activeTab, setActiveTab] = useState<DockerTab>("containers");
  const [status, setStatus] = useState<DockerStatus | null>(null);
  const [containers, setContainers] = useState<DockerContainer[]>([]);
  const [images, setImages] = useState<DockerImage[]>([]);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [statusError, setStatusError] = useState<string | null>(null);
  const [containersError, setContainersError] = useState<string | null>(null);
  const [imagesError, setImagesError] = useState<string | null>(null);
  const latestRequestRef = useRef(0);

  async function loadDocker(options: LoadOptions = {}) {
    const requestId = latestRequestRef.current + 1;
    latestRequestRef.current = requestId;

    if (options.silent) {
      setRefreshing(true);
    } else {
      setLoading(true);
    }

    const [statusResult, containersResult, imagesResult] = await Promise.allSettled([
      fetchDockerStatus(),
      fetchDockerContainers(),
      fetchDockerImages(),
    ]);

    if (latestRequestRef.current !== requestId) {
      return;
    }

    const nextStatus =
      statusResult.status === "fulfilled"
        ? statusResult.value
        : options.silent
          ? status
          : null;
    const nextContainers =
      containersResult.status === "fulfilled"
        ? containersResult.value
        : options.silent
          ? containers
          : [];
    const nextImages =
      imagesResult.status === "fulfilled"
        ? imagesResult.value
        : options.silent
          ? images
          : [];

    setStatus(nextStatus);
    setContainers(nextContainers);
    setImages(nextImages);
    setStatusError(statusResult.status === "rejected" ? getErrorMessage(statusResult.reason, "Failed to inspect Docker.") : null);

    if (nextStatus && (!nextStatus.installed || !nextStatus.service_running)) {
      setContainersError(null);
      setImagesError(null);
    } else {
      setContainersError(
        containersResult.status === "rejected"
          ? getErrorMessage(containersResult.reason, "Failed to load Docker containers.")
          : null,
      );
      setImagesError(
        imagesResult.status === "rejected"
          ? getErrorMessage(imagesResult.reason, "Failed to load Docker images.")
          : null,
      );
    }

    setLoading(false);
    setRefreshing(false);
  }

  useEffect(() => {
    void loadDocker();

    return () => {
      latestRequestRef.current += 1;
    };
  }, []);

  const canCreateContainer = Boolean(status?.installed && status.service_running);
  const actions = (
    <>
      <Button size="sm" onClick={() => setCreateDialogOpen(true)} disabled={!canCreateContainer || loading}>
        <Plus className="h-4 w-4" />
        Add Container
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={() => void loadDocker({ silent: true })}
        disabled={loading || refreshing}
      >
        {refreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
        Refresh
      </Button>
    </>
  );

  const activeDataCount = activeTab === "containers" ? containers.length : images.length;
  const activeTabError = activeTab === "containers" ? containersError : imagesError;
  const headerMeta = getPageMeta(status, containers, images, activeTab);
  const statusUnavailable = status ? !status.installed || !status.service_running : false;

  return (
    <div>
      <PageHeader title="Docker" meta={headerMeta} actions={actions} />
      <AddDockerContainerDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        onCreated={(container) => {
          toast.success(`Created container ${getContainerLabel(container)}.`);
          void loadDocker({ silent: true });
        }}
      />

      <section className="px-4 sm:px-6 lg:px-8">
        <div className="space-y-4">
          <div
            className="flex flex-col gap-2 rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] p-2 shadow-[var(--app-shadow)] sm:flex-row"
            role="tablist"
            aria-label="Docker inventory tabs"
          >
            <TabButton
              active={activeTab === "containers"}
              label="Containers"
              count={containers.length}
              onClick={() => setActiveTab("containers")}
            />
            <TabButton
              active={activeTab === "images"}
              label="Images"
              count={images.length}
              onClick={() => setActiveTab("images")}
            />
          </div>

          {statusError ? (
            <div className="rounded-xl border border-[var(--app-danger-soft)] bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-foreground">
              {statusError}
            </div>
          ) : null}

          {!statusUnavailable && activeTabError ? (
            <div className="rounded-xl border border-[var(--app-danger-soft)] bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-foreground">
              {activeTabError}
            </div>
          ) : null}

          {loading ? activeTab === "containers" ? <ContainersSkeleton /> : <ImagesSkeleton /> : null}

          {!loading && status && !status.installed ? (
            <DockerEmptyState
              title="Docker is not installed"
              description="Install Docker from the Applications page first, then container and image inventory will appear here."
            />
          ) : null}

          {!loading && status && status.installed && !status.service_running ? (
            <DockerEmptyState
              title="Docker daemon is offline"
              description="The Docker service is installed but not running, so container and image inventory are unavailable right now."
            />
          ) : null}

          {!loading && !status && statusError && activeDataCount === 0 ? (
            <DockerEmptyState
              title={`Docker ${activeTab} are unavailable`}
              description="FlowPanel could not inspect Docker right now. Try refreshing after Docker becomes reachable again."
            />
          ) : null}

          {!loading && activeTab === "containers" && !statusUnavailable && containersError && containers.length === 0 ? (
            <DockerEmptyState
              title="Docker containers are unavailable"
              description="FlowPanel could not read the container inventory right now. Try refreshing after Docker becomes reachable again."
            />
          ) : null}

          {!loading && activeTab === "images" && !statusUnavailable && imagesError && images.length === 0 ? (
            <DockerEmptyState
              title="Docker images are unavailable"
              description="FlowPanel could not read the image inventory right now. Try refreshing after Docker becomes reachable again."
            />
          ) : null}

          {!loading && activeTab === "containers" && !containersError && !statusUnavailable && containers.length === 0 ? (
            <DockerEmptyState
              title="No containers found"
              description="Containers will appear here as soon as Docker workloads are created on this node."
            />
          ) : null}

          {!loading && activeTab === "images" && !imagesError && !statusUnavailable && images.length === 0 ? (
            <DockerEmptyState
              title="No images found"
              description="Pulled and built Docker images will appear here as soon as Docker starts caching them on this node."
            />
          ) : null}

          {!loading && activeTab === "containers" && containers.length > 0 ? <ContainerList containers={containers} /> : null}
          {!loading && activeTab === "images" && images.length > 0 ? <ImageList images={images} /> : null}
        </div>
      </section>
    </div>
  );
}
