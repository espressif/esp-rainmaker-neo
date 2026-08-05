/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentProps } from "react";
import type { LucideIcon } from "lucide-react";
import type { Badge } from "@espressif/dashboard-ui-components/components";

type BadgeProps = ComponentProps<typeof Badge>;

/**
 * `pill` — the padded, rounded-full status chip used across most lists and detail
 * headers (OTA jobs, node groups, things, registration jobs). 16px icon.
 * `compact` — the tighter chip used inside settings/post-deployment cards where
 * horizontal space is scarcer. 14px icon.
 */
export type StatusBadgeSize = "pill" | "compact";

export interface StatusBadgeProps {
  /** Already-translated label. The primitive stays i18n-free. */
  label: string;
  /** Lucide icon rendered when `isLoading` is falsy. */
  Icon: LucideIcon;
  color: BadgeProps["color"];
  /** Defaults to `"soft"`; only registration-job uses `"gradient"`. */
  variant?: BadgeProps["variant"];
  /** Defaults to `"pill"`. */
  size?: StatusBadgeSize;
  /** When true, swaps `Icon` for a `Spinner` of matching size. */
  isLoading?: boolean;
  className?: string;
}
