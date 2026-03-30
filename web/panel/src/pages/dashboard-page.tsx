import { ShellPage } from "@/components/shell-page";

export function DashboardPage() {
  return (
    <ShellPage
      title="Overview"
      meta="Step 1 is complete and the admin shell starts empty."
      message="No users, domains, or jobs are seeded into FlowPanel yet. The next implementation step is SQLite migrations and durable storage."
    />
  );
}
