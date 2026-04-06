import { ShellPage } from "@/components/shell-page";

export function TerminalPage() {
  return (
    <ShellPage
      title="Terminal"
      meta="Remote shell access is not exposed in the current baseline."
      message="Terminal sessions, key management, and host connection workflows can be added here after the backend includes an audited remote access integration."
    />
  );
}
