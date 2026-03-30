import { ShellPage } from "@/components/shell-page";

export function JobsPage() {
  return (
    <ShellPage
      title="Jobs"
      meta="No background jobs are exposed in the current Step 1 baseline."
      message="The scheduler exists in the backend skeleton, but job registration, execution history, and controls are still deferred."
    />
  );
}
