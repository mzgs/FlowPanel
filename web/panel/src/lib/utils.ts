import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function sleep(ms: number = 1000) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export function getPageNumbers(currentPage: number, totalPages: number) {
  const maxVisiblePages = 5;
  const rangeWithDots: Array<number | string> = [];

  if (totalPages <= maxVisiblePages) {
    for (let i = 1; i <= totalPages; i += 1) {
      rangeWithDots.push(i);
    }
  } else {
    rangeWithDots.push(1);

    if (currentPage <= 3) {
      for (let i = 2; i <= 4; i += 1) {
        rangeWithDots.push(i);
      }
      rangeWithDots.push("...", totalPages);
    } else if (currentPage >= totalPages - 2) {
      rangeWithDots.push("...");
      for (let i = totalPages - 3; i <= totalPages; i += 1) {
        rangeWithDots.push(i);
      }
    } else {
      rangeWithDots.push("...");
      for (let i = currentPage - 1; i <= currentPage + 1; i += 1) {
        rangeWithDots.push(i);
      }
      rangeWithDots.push("...", totalPages);
    }
  }

  return rangeWithDots;
}

export async function copyTextToClipboard(text: string) {
  if (typeof navigator !== "undefined" && typeof navigator.clipboard?.writeText === "function") {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // Fall back to the legacy copy path for browsers or desktop shells that block the async Clipboard API.
    }
  }

  if (typeof document === "undefined" || !document.body) {
    throw new Error("Clipboard is unavailable.");
  }

  const textarea = document.createElement("textarea");
  const selection = document.getSelection();
  const originalRange = selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : null;
  const activeElement = document.activeElement instanceof HTMLElement ? document.activeElement : null;

  textarea.value = text;
  textarea.setAttribute("readonly", "true");
  textarea.setAttribute("aria-hidden", "true");
  textarea.style.position = "fixed";
  textarea.style.top = "0";
  textarea.style.left = "-9999px";
  textarea.style.opacity = "0";

  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  textarea.setSelectionRange(0, textarea.value.length);

  const copied = document.execCommand("copy");

  document.body.removeChild(textarea);

  if (originalRange && selection) {
    selection.removeAllRanges();
    selection.addRange(originalRange);
  }

  activeElement?.focus();

  if (!copied) {
    throw new Error("Copy failed.");
  }
}
