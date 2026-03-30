import { ShellPage } from "@/components/shell-page";

export function SshPage() {
  return (
    <ShellPage
      title="SSH"
      meta="Remote shell access is not exposed in the current baseline."
      message="Terminal sessions, key management, and host connection workflows can be added here after the backend includes an audited SSH integration."
    />
  );
}
