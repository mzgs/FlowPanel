import { ShellPage } from "@/components/shell-page";

export function SettingsPage() {
  return (
    <ShellPage
      title="Settings"
      meta="No panel settings are exposed in the current Step 1 baseline."
      message="Runtime controls, session management, and deployment settings will appear once the backend moves past the bootstrap phase."
    />
  );
}
