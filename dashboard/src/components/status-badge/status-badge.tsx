/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Badge, Spinner } from "@espressif/dashboard-ui-components/components";
import { cn } from "@/utils/utils";
import type { StatusBadgeProps, StatusBadgeSize } from "./status-badge.props";

/** Size-specific class + pixel tokens. Keeps the JSX free of ternaries. */
const SIZE_STYLES: Record<
  StatusBadgeSize,
  { badge: string; icon: string; iconPx: number }
> = {
  pill: {
    badge: "rounded-full pl-0.5 pr-1.75 py-0.5",
    icon: "h-4 w-4",
    iconPx: 16,
  },
  compact: {
    // Sibling badges rely on the default `Badge` rounding — no override needed.
    badge: "",
    icon: "h-3.5 w-3.5",
    iconPx: 14,
  },
};

/**
 * Shared icon+label status chip. Domain wrappers (ota-job, node-group, thing, …)
 * resolve their per-status presentation and translation and hand the results in
 * here — keeps every status badge visually consistent and lets us fix drift in
 * one place instead of seven.
 */
export function StatusBadge({
  label,
  Icon,
  color,
  variant = "gradient",
  size = "pill",
  isLoading = false,
  className,
}: StatusBadgeProps) {
  const styles = SIZE_STYLES[size];

  return (
    <Badge
      color={color}
      variant={variant}
      className={cn("font-normal gap-1.5", styles.badge, className)}
    >
      {isLoading ? (
        <Spinner size={styles.iconPx} className="shrink-0" aria-hidden />
      ) : (
        <Icon className={cn(styles.icon, "shrink-0")} aria-hidden />
      )}
      {label}
    </Badge>
  );
}
