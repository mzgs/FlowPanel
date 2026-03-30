import { ShellPage } from "@/components/shell-page";

export function DomainsPage() {
  return (
    <ShellPage
      title="Domains"
      meta="No domain records exist in the current Step 1 baseline."
      message="This view remains empty until domain storage, hostname validation, and upstream routing are added."
    />
  );
}
