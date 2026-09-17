import { useEffect, useState } from "react";

export function useTicker(ms = 1000) {
  const [, tick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => tick((t) => t + 1), ms);
    return () => clearInterval(id);
  }, [ms]);
}

export function formatAgo(date: Date | string | null): string {
  if (!date) return "never";
  const d = typeof date === "string" ? new Date(date) : date;
  const s = Math.max(0, Math.round((Date.now() - d.getTime()) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

export function useAgo(date: Date | string | null) {
  useTicker(1000);
  return formatAgo(date);
}

export function formatType(type: string): string {
  const words = type.replace(/[_-]+/g, " ").trim().toLowerCase();
  return words.charAt(0).toUpperCase() + words.slice(1);
}
