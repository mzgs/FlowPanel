type FlowPanelMarkProps = {
  className?: string;
};

export function FlowPanelMark({ className = "h-9 w-9" }: FlowPanelMarkProps) {
  return <img src="/flowpanel-icon.png" alt="" aria-hidden="true" className={className} />;
}
