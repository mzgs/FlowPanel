import {
  Link,
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  useLocation,
} from "@tanstack/react-router";
import {
  ChevronRight,
  Globe,
  LayoutDashboard,
  Menu,
  Settings,
  TimerReset,
  X,
} from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { DashboardPage } from "@/pages/dashboard-page";
import { DomainsPage } from "@/pages/domains-page";
import { JobsPage } from "@/pages/jobs-page";
import { SettingsPage } from "@/pages/settings-page";

const navigationItems = [
  { to: "/", label: "Overview", icon: LayoutDashboard },
  { to: "/domains", label: "Domains", icon: Globe },
  { to: "/jobs", label: "Jobs", icon: TimerReset },
  { to: "/settings", label: "Settings", icon: Settings },
] as const;

function RootLayout() {
  const [menuOpen, setMenuOpen] = useState(false);
  const location = useLocation();

  return (
    <div className="min-h-screen bg-[var(--app-bg)] text-[var(--app-text)]">
      <div className="md:hidden">
        <div className="flex items-center justify-between border-b border-[var(--app-border)] bg-[var(--app-surface)] px-4 py-3">
          <div className="text-[15px] font-semibold">FlowPanel</div>
          <Button variant="secondary" size="sm" onClick={() => setMenuOpen((open) => !open)}>
            {menuOpen ? <X className="h-4 w-4" /> : <Menu className="h-4 w-4" />}
          </Button>
        </div>
        {menuOpen ? (
          <nav className="border-b border-[var(--app-border)] bg-[var(--app-surface)] p-3">
            <div className="space-y-1">
              {navigationItems.map((item) => {
                const Icon = item.icon;
                const active = location.pathname === item.to;

                return (
                  <Link
                    key={item.to}
                    to={item.to}
                    onClick={() => setMenuOpen(false)}
                    className={cn(
                      "flex items-center gap-3 rounded-[10px] px-3 py-2 text-[13px] font-medium",
                      active
                        ? "bg-[var(--app-accent)] text-[#f7fbff]"
                        : "text-[var(--app-text-muted)] hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)]",
                    )}
                  >
                    <Icon className="h-4 w-4" />
                    {item.label}
                  </Link>
                );
              })}
            </div>
          </nav>
        ) : null}
      </div>

      <div className="mx-auto md:grid md:min-h-screen md:grid-cols-[var(--app-sidebar-width)_minmax(0,1fr)]">
        <aside className="hidden border-r border-[var(--app-border)] bg-[var(--app-surface)] md:block">
          <div className="border-b border-[var(--app-border)] px-5 py-5">
            <div className="text-[15px] font-semibold tracking-[-0.02em]">FlowPanel</div>
          </div>
          <nav className="p-3">
            <div className="space-y-1">
              {navigationItems.map((item) => {
                const Icon = item.icon;
                const active = location.pathname === item.to;

                return (
                  <Link
                    key={item.to}
                    to={item.to}
                    className={cn(
                      "flex items-center justify-between rounded-[10px] px-3 py-2.5 text-[13px] font-medium transition-colors duration-150",
                      active
                        ? "bg-[var(--app-accent)] text-[#f7fbff]"
                        : "text-[var(--app-text-muted)] hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)]",
                    )}
                  >
                    <span className="flex items-center gap-3">
                      <Icon className="h-4 w-4" />
                      {item.label}
                    </span>
                    {active ? <ChevronRight className="h-4 w-4" /> : null}
                  </Link>
                );
              })}
            </div>
          </nav>
        </aside>

        <main className="min-w-0">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

function RouteError() {
  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <div className="w-full max-w-md rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-6 shadow-[var(--app-shadow)]">
        <div className="mb-2 text-[18px] font-semibold">Route error</div>
        <p className="text-[13px] leading-6 text-[var(--app-text-muted)]">
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

const routeTree = rootRoute.addChildren([
  indexRoute,
  domainsRoute,
  jobsRoute,
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
