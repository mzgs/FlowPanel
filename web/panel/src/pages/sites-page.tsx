import { ShellPage } from "@/components/shell-page";

export function SitesPage() {
  return (
    <ShellPage
      title="Sites"
      meta="No site records exist in the current Step 1 baseline."
      message="This screen stays empty until the SQLite schema and site CRUD endpoints are implemented."
    />
  );
}
