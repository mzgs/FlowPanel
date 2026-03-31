import {
  Link,
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  useLocation,
} from "@tanstack/react-router";
import {
  Bell,
  Database,
  ChevronRight,
  FolderOpen,
  Globe,
  LayoutDashboard,
  Menu,
  Search,
  Settings,
  TerminalSquare,
  TimerReset,
  X,
} from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/panel/button";
import { cn } from "@/lib/utils";
import { DatabasePage } from "@/pages/database-page";
import { DashboardPage } from "@/pages/dashboard-page";
import { DomainsPage } from "@/pages/domains-page";
import { FilesPage } from "@/pages/files-page";
import { JobsPage } from "@/pages/jobs-page";
import { SettingsPage } from "@/pages/settings-page";
import { SshPage } from "@/pages/ssh-page";

const navigationItems = [
  { to: "/", label: "Overview", icon: LayoutDashboard },
  { to: "/database", label: "Database", icon: Database },
  { to: "/domains", label: "Domains", icon: Globe },
  { to: "/files", label: "Files", icon: FolderOpen },
  { to: "/jobs", label: "Jobs", icon: TimerReset },
  { to: "/ssh", label: "SSH", icon: TerminalSquare },
  { to: "/settings", label: "Settings", icon: Settings },
] as const;

function RootLayout() {
  const [menuOpen, setMenuOpen] = useState(false);
  const location = useLocation();
  const isNavItemActive = (to: string) =>
    location.pathname === to || (to === "/files" && location.pathname === "/file-manager");

  return (
    <div className="min-h-screen bg-[var(--app-bg)] text-[var(--app-text)]">
      <header className="fixed inset-x-0 top-0 z-30 border-b border-[var(--app-border)] bg-[var(--app-surface)] shadow-[var(--app-shadow)]">
        <div className="flex h-16 items-center justify-between gap-3 px-4 sm:px-6 lg:px-8">
          <div className="flex min-w-0 items-center gap-3">
            <Button
              variant="secondary"
              size="sm"
              className="lg:hidden"
              onClick={() => setMenuOpen((open) => !open)}
            >
              {menuOpen ? <X className="h-4 w-4" /> : <Menu className="h-4 w-4" />}
            </Button>
            <Link to="/" className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-[var(--app-accent)] text-white shadow-sm">
                <LayoutDashboard className="h-5 w-5" />
              </div>
              <div className="min-w-0">
                <div className="text-[15px] font-semibold tracking-[-0.02em]">FlowPanel</div>
                <div className="text-[12px] text-[var(--app-text-muted)]">Admin dashboard</div>
              </div>
            </Link>
          </div>

          <div className="hidden flex-1 px-4 lg:flex lg:max-w-xl">
            <label className="relative block w-full">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--app-text-muted)]" />
              <input
                readOnly
                value=""
                placeholder="Navigation, files, and runtime tools"
                className="h-10 w-full rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] pl-10 pr-4 text-[14px] text-[var(--app-text)] outline-none"
              />
            </label>
          </div>

          <div className="flex items-center gap-2">
            <div className="hidden rounded-full border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-1 text-[12px] font-medium text-[var(--app-text-muted)] md:block">
              Local node
            </div>
            <button
              type="button"
              className="inline-flex h-10 w-10 items-center justify-center rounded-xl border border-[var(--app-border)] bg-[var(--app-surface)] text-[var(--app-text-muted)] transition-colors hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)]"
            >
              <Bell className="h-4 w-4" />
              <span className="sr-only">Notifications</span>
            </button>
          </div>
        </div>
      </header>

      {menuOpen ? (
        <button
          type="button"
          className="fixed inset-0 z-10 bg-slate-900/20 lg:hidden"
          onClick={() => setMenuOpen(false)}
          aria-label="Close navigation"
        />
      ) : null}

      <aside
        className={cn(
          "fixed left-0 top-16 z-20 flex h-[calc(100vh-var(--app-navbar-height))] w-64 flex-col border-r border-[var(--app-border)] bg-[var(--app-surface)] transition-transform duration-200 lg:translate-x-0",
          menuOpen ? "translate-x-0" : "-translate-x-full",
        )}
      >
        <div className="flex-1 overflow-y-auto px-3 py-5">
          <div className="mb-5 rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
            <div className="text-[12px] font-semibold uppercase tracking-[0.18em] text-[var(--app-accent)]">
              Workspace
            </div>
            <div className="mt-2 text-[15px] font-semibold text-[var(--app-text)]">Server controls</div>
            <p className="mt-1 text-[13px] leading-6 text-[var(--app-text-muted)]">
              Runtime status, domains, files, and operations in one panel.
            </p>
          </div>

          <nav className="space-y-1">
            {navigationItems.map((item) => {
              const Icon = item.icon;
              const active = isNavItemActive(item.to);

              return (
                <Link
                  key={item.to}
                  to={item.to}
                  onClick={() => setMenuOpen(false)}
                  className={cn(
                    "flex items-center justify-between rounded-xl px-3 py-2.5 text-[14px] font-medium transition-colors duration-150",
                    active
                      ? "bg-[var(--app-accent-soft)] text-[var(--app-accent)]"
                      : "text-[var(--app-text-muted)] hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)]",
                  )}
                >
                  <span className="flex items-center gap-3">
                    <Icon className={cn("h-5 w-5", active ? "text-[var(--app-accent)]" : "")} />
                    {item.label}
                  </span>
                  {active ? <ChevronRight className="h-4 w-4" /> : null}
                </Link>
              );
            })}
          </nav>
        </div>
      </aside>

      <main className="min-w-0 pt-16 lg:pl-64">
        <Outlet />
      </main>
    </div>
  );
}

function RouteError() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-[var(--app-bg)] px-4">
      <div className="w-full max-w-md rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] p-6 shadow-[var(--app-shadow)]">
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

const databaseRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/database",
  component: DatabasePage,
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

const jobsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/jobs",
  component: JobsPage,
});

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  component: SettingsPage,
});

const sshRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/ssh",
  component: SshPage,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  databaseRoute,
  domainsRoute,
  filesRoute,
  legacyFileManagerRoute,
  jobsRoute,
  sshRoute,
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
