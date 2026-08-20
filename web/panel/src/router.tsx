import {
  Link,
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  useLocation,
  useNavigate,
} from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Fragment,
  Suspense,
  lazy,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentType,
  type KeyboardEvent,
} from "react";
import { fetchAuthSession, logout } from "@/api/auth";
import { fetchSettings, panelSettingsQueryKey } from "@/api/settings";
import { AuthPage } from "@/pages/auth-page";
import {
  Bell,
  Clock,
  Database,
  ChevronRight,
  Docker,
  FolderOpen,
  HardDrive,
  LayoutDashboard,
  List,
  LogOut,
  Monitor,
  Package,
  Search,
  Server,
  Settings,
  TerminalSquare,
  World,
} from "@/components/icons/lucide-icons";
import { FlowPanelMark } from "@/components/flowpanel-mark";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
} from "@/components/ui/sidebar";

function RoutePending() {
  return (
    <div className="px-4 py-4 text-sm text-muted-foreground sm:px-6 lg:px-8">
      Loading...
    </div>
  );
}

function lazyRouteComponent(loader: () => Promise<{ default: ComponentType }>) {
  const Page = lazy(loader);

  return function LazyRouteComponent() {
    return (
      <Suspense fallback={<RoutePending />}>
        <Page />
      </Suspense>
    );
  };
}

const DashboardPage = lazyRouteComponent(() =>
  import("@/pages/dashboard-page").then((module) => ({ default: module.DashboardPage })),
);
const DomainsPage = lazyRouteComponent(() =>
  import("@/pages/domains-page").then((module) => ({ default: module.DomainsPage })),
);
const DomainDetailPage = lazyRouteComponent(() =>
  import("@/pages/domain-detail-page").then((module) => ({ default: module.DomainDetailPage })),
);
const LogsPage = lazyRouteComponent(() =>
  import("@/pages/logs-page").then((module) => ({ default: module.LogsPage })),
);
const DatabasePage = lazyRouteComponent(() =>
  import("@/pages/database-page").then((module) => ({ default: module.DatabasePage })),
);
const DockerPage = lazyRouteComponent(() =>
  import("@/pages/docker-page").then((module) => ({ default: module.DockerPage })),
);
const FTPPage = lazyRouteComponent(() =>
  import("@/pages/ftp-page").then((module) => ({ default: module.FTPPage })),
);
const FilesPage = lazyRouteComponent(() =>
  import("@/pages/files-page").then((module) => ({ default: module.FilesPage })),
);
const ApplicationsPage = lazyRouteComponent(() =>
  import("@/pages/applications-page").then((module) => ({ default: module.ApplicationsPage })),
);
const CronPage = lazyRouteComponent(() =>
  import("@/pages/cron-page").then((module) => ({ default: module.CronPage })),
);
const TaskManagerPage = lazyRouteComponent(() =>
  import("@/pages/system-page").then((module) => ({ default: module.TaskManagerPage })),
);
const TerminalPage = lazyRouteComponent(() =>
  import("@/pages/terminal-page").then((module) => ({ default: module.TerminalPage })),
);
const ActivityPage = lazyRouteComponent(() =>
  import("@/pages/activity-page").then((module) => ({ default: module.ActivityPage })),
);
const BackupsPage = lazyRouteComponent(() =>
  import("@/pages/backups-page").then((module) => ({ default: module.BackupsPage })),
);
const SettingsPage = lazyRouteComponent(() =>
  import("@/pages/settings-page").then((module) => ({ default: module.SettingsPage })),
);
const navigationItems = [
  { to: "/", label: "Overview", icon: LayoutDashboard },
  { to: "/domains", label: "Domains", icon: World },
  { to: "/database", label: "Database", icon: Database },
  { to: "/docker", label: "Docker", icon: Docker },
  { to: "/ftp", label: "FTP", icon: Server },
  { to: "/files", label: "Files", icon: FolderOpen },
  { to: "/applications", label: "Applications", icon: Package },
  { to: "/cron", label: "Cron", icon: Clock },
  { to: "/task-manager", label: "System", icon: Monitor },

  { to: "/terminal", label: "Terminal", icon: TerminalSquare },
  { to: "/activity", label: "Activity", icon: List },
  { to: "/backups", label: "Backups", icon: HardDrive },
  { to: "/settings", label: "Settings", icon: Settings },
] as const;

function formatSegmentLabel(segment: string) {
  if (segment === "") {
    return "Overview";
  }

  if (segment === "jobs") {
    return "Cron";
  }

  if (segment === "file-manager") {
    return "Files";
  }

  if (segment === "ftp") {
    return "FTP";
  }

  return decodeURIComponent(segment)
    .replace(/-/g, " ")
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

function getBreadcrumbs(pathname: string) {
  if (pathname === "/") {
    return [{ to: "/", label: "Overview" }];
  }
  if (pathname === "/security") {
    return [
      { to: "/task-manager", label: "System" },
      { to: "/security", label: "Firewall" },
    ];
  }

  const segments = pathname.split("/").filter(Boolean);
  if (segments[0] === "domains") {
    const hostname = segments[1];
    const breadcrumbs = [{ to: "/domains", label: "Domains" }];

    if (hostname) {
      breadcrumbs.push({
        to: `/domains/${hostname}`,
        label: decodeURIComponent(hostname),
      });
    }

    if (segments[2]) {
      breadcrumbs.push({
        to: `/domains/${hostname}/${segments[2]}`,
        label: formatSegmentLabel(segments[2]),
      });
    }

    return breadcrumbs;
  }

  let currentPath = "";
  return segments.map((segment) => {
    currentPath += `/${segment}`;
    return {
      to: segment === "jobs" ? "/cron" : segment === "file-manager" ? "/files" : currentPath,
      label: formatSegmentLabel(segment),
    };
  });
}

function RootLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const searchInputRef = useRef<HTMLInputElement>(null);
  const [panelSearch, setPanelSearch] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [selectedSearchIndex, setSelectedSearchIndex] = useState(0);
  const authQuery = useQuery({
    queryKey: ["auth", "session"],
    queryFn: fetchAuthSession,
    retry: false,
  });
  const settingsQuery = useQuery({
    queryKey: panelSettingsQueryKey,
    queryFn: fetchSettings,
    enabled: Boolean(authQuery.data?.authenticated),
  });
  const logoutMutation = useMutation({
    mutationFn: logout,
    onSuccess: (session) => {
      queryClient.setQueryData(["auth", "session"], session);
    },
  });
  const breadcrumbs = getBreadcrumbs(location.pathname);
  const currentPageName = breadcrumbs[breadcrumbs.length - 1]?.label;
  const panelName = settingsQuery.data?.panel_name || "FlowPanel";
  const searchResults = useMemo(() => {
    const query = panelSearch.trim().toLowerCase();

    return query
      ? navigationItems.filter(
          (item) => item.label.toLowerCase().includes(query) || item.to.toLowerCase().includes(query),
        )
      : navigationItems;
  }, [panelSearch]);
  const isNavItemActive = (to: string) =>
    location.pathname === to ||
    (to === "/domains" && location.pathname.startsWith("/domains/")) ||
    (to === "/task-manager" && location.pathname === "/security") ||
    (to === "/files" && location.pathname === "/file-manager") ||
    (to === "/cron" && location.pathname === "/jobs");

  useEffect(() => {
    if (!authQuery.data?.authenticated) {
      return;
    }

    const focusSearch = (event: globalThis.KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        searchInputRef.current?.focus();
      }
    };

    window.addEventListener("keydown", focusSearch);
    return () => window.removeEventListener("keydown", focusSearch);
  }, [authQuery.data?.authenticated]);

  useEffect(() => {
    setSelectedSearchIndex(0);
  }, [panelSearch]);

  useEffect(() => {
    document.title = currentPageName ? `${currentPageName} · ${panelName}` : panelName;
  }, [currentPageName, panelName]);

  const selectSearchResult = (index: number) => {
    const result = searchResults[index];
    if (!result) {
      return;
    }

    void navigate({ to: result.to });
    setPanelSearch("");
    setSearchOpen(false);
    searchInputRef.current?.blur();
  };

  const handleSearchKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      setSelectedSearchIndex((current) =>
        searchResults.length ? (current + direction + searchResults.length) % searchResults.length : 0,
      );
    } else if (event.key === "Enter") {
      event.preventDefault();
      selectSearchResult(selectedSearchIndex);
    } else if (event.key === "Escape") {
      setSearchOpen(false);
      searchInputRef.current?.blur();
    }
  };

  if (authQuery.isLoading) {
    return <AuthLoading />;
  }

  if (authQuery.isError) {
    return <AuthUnavailable onRetry={() => authQuery.refetch()} />;
  }

  if (!authQuery.data?.authenticated) {
    return <AuthPage setupRequired={Boolean(authQuery.data?.setup_required)} />;
  }

  return (
    <SidebarProvider defaultOpen>
      <Sidebar>
        <SidebarHeader>
          <div className="px-2 py-1">
            <Link to="/" className="flex items-center gap-3">
              <FlowPanelMark />
              <div className="min-w-0">
                <div className="truncate text-sm font-semibold tracking-tight">{panelName}</div>
                <div className="text-xs text-muted-foreground">Admin panel</div>
              </div>
            </Link>
          </div>
        </SidebarHeader>

        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupLabel>Navigation</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
              {navigationItems.map((item) => {
                const Icon = item.icon;
                const active = isNavItemActive(item.to);

                return (
                  <SidebarMenuItem key={item.to}>
                    <SidebarMenuButton asChild isActive={active} tooltip={item.label}>
                      <Link to={item.to}>
                        <Icon />
                        <span>{item.label}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>

        <SidebarFooter>
          <div className="flex items-center gap-2 rounded-md border bg-[var(--app-surface)] px-2 py-2 text-sm">
            <div className="min-w-0 flex-1">
              <div className="truncate text-xs font-medium text-foreground">
                {authQuery.data.user?.username ?? "Admin"}
              </div>
              <div className="text-[11px] text-muted-foreground">Local node</div>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-7 w-7 shrink-0"
              disabled={logoutMutation.isPending}
              onClick={() => logoutMutation.mutate()}
              title="Sign out"
              aria-label="Sign out"
            >
              <LogOut className="h-4 w-4" />
            </Button>
          </div>
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>

      <SidebarInset className="@container/content">
        <header className="sticky top-0 z-20 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
          <div className="flex h-16 items-center justify-between gap-3 px-4 sm:px-6 lg:px-8">
            <div className="flex min-w-0 items-center gap-3">
              <SidebarTrigger />
              <Separator orientation="vertical" className="h-4" />

              <div className="min-w-0">
                <Link
                  to="/"
                  className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground transition-colors hover:text-foreground"
                >
                  Control center
                </Link>
                <div className="flex min-w-0 flex-wrap items-center gap-2 text-sm font-medium text-foreground">
                  {breadcrumbs.map((crumb, index) => (
                    <Fragment key={crumb.to}>
                      {index > 0 ? (
                        <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                      ) : null}
                      <Link
                        to={crumb.to}
                        className={
                          index === breadcrumbs.length - 1
                            ? "truncate text-muted-foreground transition-colors hover:text-foreground"
                            : "truncate transition-colors hover:text-primary"
                        }
                      >
                        {crumb.label}
                      </Link>
                    </Fragment>
                  ))}
                </div>
              </div>
            </div>

            <div className="flex items-center gap-2">
              <div
                className="relative hidden w-64 lg:block"
                onBlur={(event) => {
                  if (!event.currentTarget.contains(event.relatedTarget)) {
                    setSearchOpen(false);
                  }
                }}
              >
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <input
                  ref={searchInputRef}
                  value={panelSearch}
                  onChange={(event) => setPanelSearch(event.target.value)}
                  onFocus={() => setSearchOpen(true)}
                  onKeyDown={handleSearchKeyDown}
                  placeholder="Search panel..."
                  role="combobox"
                  aria-label="Search panel pages"
                  aria-autocomplete="list"
                  aria-expanded={searchOpen}
                  aria-controls="panel-search-results"
                  aria-activedescendant={
                    searchOpen && searchResults[selectedSearchIndex]
                      ? `panel-search-result-${selectedSearchIndex}`
                      : undefined
                  }
                  className="h-9 w-full rounded-md border border-input bg-transparent pl-10 pr-12 text-sm text-foreground outline-none placeholder:text-muted-foreground focus:border-ring focus:ring-2 focus:ring-ring/20 dark:bg-input/30"
                />
                <kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 rounded border bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                  ⌘K
                </kbd>

                {searchOpen ? (
                  <div
                    id="panel-search-results"
                    role="listbox"
                    className="absolute right-0 top-[calc(100%+0.375rem)] z-50 max-h-72 w-full overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-md"
                  >
                    {searchResults.length ? (
                      searchResults.map((item, index) => {
                        const Icon = item.icon;
                        return (
                          <button
                            key={item.to}
                            id={`panel-search-result-${index}`}
                            type="button"
                            role="option"
                            aria-selected={index === selectedSearchIndex}
                            onMouseDown={(event) => event.preventDefault()}
                            onMouseEnter={() => setSelectedSearchIndex(index)}
                            onClick={() => selectSearchResult(index)}
                            className={`flex w-full items-center gap-2 rounded-sm px-2 py-2 text-left text-sm ${
                              index === selectedSearchIndex ? "bg-accent text-accent-foreground" : "hover:bg-accent"
                            }`}
                          >
                            <Icon className="h-4 w-4 text-muted-foreground" />
                            <span>{item.label}</span>
                          </button>
                        );
                      })
                    ) : (
                      <div className="px-2 py-3 text-center text-sm text-muted-foreground">No panel pages found.</div>
                    )}
                  </div>
                ) : null}
              </div>
              <Button variant="ghost" size="icon">
                <Bell className="h-4 w-4" />
                <span className="sr-only">Notifications</span>
              </Button>
            </div>
          </div>
        </header>

        <main className="min-w-0 pb-10">
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}

function AuthLoading() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-[var(--app-bg)] px-4">
      <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-3 text-sm text-muted-foreground">
        Loading FlowPanel...
      </div>
    </main>
  );
}

function AuthUnavailable({ onRetry }: { onRetry: () => void }) {
  return (
    <main className="flex min-h-screen items-center justify-center bg-[var(--app-bg)] px-4">
      <div className="w-full max-w-[360px] rounded-lg border border-[var(--app-border)] bg-[var(--app-bg-2)] p-5 shadow-[var(--app-shadow)]">
        <div className="text-[17px] font-semibold text-foreground">Auth unavailable</div>
        <p className="mt-1 text-[13px] leading-5 text-muted-foreground">
          FlowPanel could not load the current session.
        </p>
        <Button type="button" className="mt-4 h-9 w-full" onClick={onRetry}>
          Retry
        </Button>
      </div>
    </main>
  );
}

function RouteError() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-[var(--app-bg)] px-4">
      <div className="w-full max-w-md rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] p-6 shadow-[var(--app-shadow)]">
        <div className="mb-2 text-[18px] font-semibold">Route error</div>
        <p className="text-[14px] leading-6 text-[var(--app-text-muted)]">
          The requested panel view could not be rendered.
        </p>
      </div>
    </div>
  );
}

const rootRoute = createRootRoute({
  component: RootLayout,
  errorComponent: RouteError,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: DashboardPage,
});

const domainsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/domains",
  component: DomainsPage,
});

const activityRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/activity",
  component: ActivityPage,
});

const logsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/domains/$hostname/logs",
  component: LogsPage,
});

const domainDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/domains/$hostname",
  component: DomainDetailPage,
});

const databaseRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/database",
  component: DatabasePage,
});

const dockerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/docker",
  component: DockerPage,
});

const applicationsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/applications",
  component: ApplicationsPage,
});

const ftpRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/ftp",
  component: FTPPage,
});

const backupsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/backups",
  component: BackupsPage,
});

const filesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/files",
  component: FilesPage,
});

const legacyFileManagerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/file-manager",
  component: FilesPage,
});

const cronRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/cron",
  component: CronPage,
});

const legacyJobsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/jobs",
  component: CronPage,
});

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  component: SettingsPage,
});

const securityRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/security",
  component: TaskManagerPage,
});

const taskManagerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/task-manager",
  component: TaskManagerPage,
});

const terminalRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/terminal",
  component: TerminalPage,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  activityRoute,
  logsRoute,
  domainsRoute,
  domainDetailRoute,
  databaseRoute,
  dockerRoute,
  applicationsRoute,
  ftpRoute,
  backupsRoute,
  filesRoute,
  legacyFileManagerRoute,
  cronRoute,
  legacyJobsRoute,
  taskManagerRoute,
  terminalRoute,
  securityRoute,
  settingsRoute,
]);

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
