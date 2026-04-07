import { PageHeader } from "@/components/page-header";
import { TerminalWindow } from "@/components/terminal-window";

export function TerminalPage() {
  return (
    <>
      <PageHeader title="Terminal" />

      <div className="px-4 pb-6 sm:px-6 lg:px-8">
        <TerminalWindow />
      </div>
    </>
  );
}
