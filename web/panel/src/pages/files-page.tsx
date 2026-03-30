import { ShellPage } from "@/components/shell-page";

export function FilesPage() {
  return (
    <ShellPage
      title="Files"
      meta="File operations are not exposed in the current baseline."
      message="Directory browsing, upload flows, permissions, and file edits can be surfaced here once the panel defines a safe filesystem access model."
    />
  );
}
