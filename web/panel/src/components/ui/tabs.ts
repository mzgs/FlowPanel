const segmentedTabListClassName =
  "inline-flex items-center rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-1";

const segmentedTabClassName =
  "inline-flex h-8 items-center justify-center gap-1.5 rounded-md px-3 text-[12px] font-medium text-[var(--app-text-muted)] outline-none transition-colors hover:text-[var(--app-text)] focus-visible:ring-2 focus-visible:ring-ring data-[active=true]:bg-[var(--app-bg-2)] data-[active=true]:text-[var(--app-text)] data-[active=true]:shadow-sm";

const underlineTabListClassName =
  "flex items-center gap-4 overflow-x-auto border-b border-[var(--app-border)]";

const underlineTabClassName =
  "inline-flex items-center gap-1.5 whitespace-nowrap border-b-2 border-transparent px-1 py-2.5 text-sm font-medium text-[var(--app-text-muted)] outline-none transition-colors hover:text-[var(--app-text)] focus-visible:ring-2 focus-visible:ring-ring data-[active=true]:border-[var(--app-text)] data-[active=true]:text-[var(--app-text)]";

export {
  segmentedTabClassName,
  segmentedTabListClassName,
  underlineTabClassName,
  underlineTabListClassName,
};
