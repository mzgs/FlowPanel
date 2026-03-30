import { ShellPage } from "@/components/shell-page";

export function DatabasePage() {
  return (
    <ShellPage
      title="Database"
      meta="Database tools are not exposed in the current baseline."
      message="Schema browsing, query tools, backups, and migration controls can be added here once the panel starts managing SQLite operations directly."
    />
  );
}
