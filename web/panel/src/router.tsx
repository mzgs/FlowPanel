import {
  Link,
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  useLocation,
} from "@tanstack/react-router";
import { Fragment } from "react";
import {
  Bell,
  Clock,
  Database,
  ChevronRight,
  FolderOpen,
  HardDrive,
  LayoutDashboard,
  List,
  Package,
  Search,
  Server,
  Settings,
  TerminalSquare,
  World,
} from "@/components/icons/tabler-icons";
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
import { CronPage } from "@/pages/cron-page";
import { DomainDetailPage } from "@/pages/domain-detail-page";
import { ActivityPage } from "@/pages/activity-page";
import { ApplicationsPage } from "@/pages/applications-page";
import { BackupsPage } from "@/pages/backups-page";
import { DatabasePage } from "@/pages/database-page";
import { DashboardPage } from "@/pages/dashboard-page";
import { DomainsPage } from "@/pages/domains-page";
import { FilesPage } from "@/pages/files-page";
import { FTPPage } from "@/pages/ftp-page";
import { LogsPage } from "@/pages/logs-page";
import { SettingsPage } from "@/pages/settings-page";
import { TerminalPage } from "@/pages/terminal-page";

const navigationItems = [
  { to: "/", label: "Overview", icon: LayoutDashboard },
  { to: "/domains", label: "Domains", icon: World },
  { to: "/database", label: "Database", icon: Database },
  { to: "/applications", label: "Applications", icon: Package },
  { to: "/ftp", label: "FTP", icon: Server },
  { to: "/backups", label: "Backups", icon: HardDrive },
  { to: "/files", label: "Files", icon: FolderOpen },
  { to: "/cron", label: "Cron", icon: Clock },
  { to: "/terminal", label: "Terminal", icon: TerminalSquare },
  { to: "/activity", label: "Activity", icon: List },
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
  const breadcrumbs = getBreadcrumbs(location.pathname);
  const isNavItemActive = (to: string) =>
    location.pathname === to ||
    (to === "/domains" && location.pathname.startsWith("/domains/")) ||
    (to === "/files" && location.pathname === "/file-manager") ||
    (to === "/cron" && location.pathname === "/jobs");

  return (
    <SidebarProvider defaultOpen>
      <Sidebar>
        <SidebarHeader>
          <div className="px-2 py-1">
            <Link to="/" className="flex items-center gap-3">
              <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                <LayoutDashboard className="h-4 w-4" />
              </div>
              <div className="min-w-0">
                <div className="text-sm font-semibold tracking-tight">FlowPanel</div>
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
          <div className="rounded-md border bg-[var(--app-surface)] px-3 py-2 text-sm text-muted-foreground">
            Local node
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

            <div className="hidden flex-1 px-4 lg:flex lg:max-w-xl">
              <label className="relative block w-full">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <input
                  readOnly
                  value=""
                  placeholder="Search panel actions, files, and services"
                  className="h-9 w-full rounded-md border border-input bg-transparent pl-10 pr-4 text-sm text-foreground outline-none placeholder:text-muted-foreground dark:bg-input/30"
                />
              </label>
            </div>

            <div className="flex items-center gap-2">
              <div className="hidden rounded-md border bg-[var(--app-surface)] px-3 py-1.5 text-xs text-muted-foreground md:block">
                Local workspace
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
  applicationsRoute,
  ftpRoute,
  backupsRoute,
  filesRoute,
  legacyFileManagerRoute,
  cronRoute,
  legacyJobsRoute,
  terminalRoute,
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
