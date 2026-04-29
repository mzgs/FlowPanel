import { useLocation } from "@tanstack/react-router";
import { DatabasePanel } from "@/components/database-panel";

export function DatabasePage() {
  const requestedDomainFilter = useLocation({
    select: (location) => {
      const domain = location.search.domain;
      return typeof domain === "string" ? domain.trim() : "";
    },
  });

  return <DatabasePanel initialSearch={requestedDomainFilter} />;
}
