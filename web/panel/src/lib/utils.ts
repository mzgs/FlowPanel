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
