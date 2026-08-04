/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LucideIcon } from "lucide-react";
import { CheckCircle2, CircleDashed, LoaderCircle } from "lucide-react";
import type { DynamicGroupStatus } from "@aws-sdk/client-iot";
import type { Color } from "@espressif/dashboard-ui-components";

export interface NodeGroupStatusPresentation {
  Icon: LucideIcon;
  color: Color;
  /** Transient states render a spinner in place of {@link Icon}. */
  spinning?: boolean;
  /** i18n key under the `node-groups` namespace. Empty signals an unmapped status. */
  i18nKey: string;
  labelFallback: string;
}

export const NODE_GROUP_STATUS_PRESENTATION: Record<
  DynamicGroupStatus,
  NodeGroupStatusPresentation
> = {
  ACTIVE: {
    Icon: CheckCircle2,
    color: "success",
    i18nKey: "node-groups:details.status.ACTIVE",
    labelFallback: "Active",
  },
  BUILDING: {
    Icon: LoaderCircle,
    color: "info",
    spinning: true,
    i18nKey: "node-groups:details.status.BUILDING",
    labelFallback: "Building",
  },
  REBUILDING: {
    Icon: LoaderCircle,
    color: "warning",
    spinning: true,
    i18nKey: "node-groups:details.status.REBUILDING",
    labelFallback: "Rebuilding",
  },
};

/**
 * Statuses AWS resolves on its own within seconds. While a group sits in one of
 * these, its membership is still being evaluated, so the details page polls.
 */
export const TRANSIENT_NODE_GROUP_STATUSES: readonly DynamicGroupStatus[] = [
  "BUILDING",
  "REBUILDING",
];

const UNKNOWN_STATUS_PRESENTATION: NodeGroupStatusPresentation = {
  Icon: CircleDashed,
  color: "gray",
  i18nKey: "",
  labelFallback: "",
};

export function isNodeGroupStatus(
  status: string | null | undefined,
): status is DynamicGroupStatus {
  return !!status && status in NODE_GROUP_STATUS_PRESENTATION;
}

export function isTransientNodeGroupStatus(
  status: string | null | undefined,
): boolean {
  return (
    isNodeGroupStatus(status) && TRANSIENT_NODE_GROUP_STATUSES.includes(status)
  );
}

export function getNodeGroupStatusPresentation(
  status: string | null | undefined,
): NodeGroupStatusPresentation {
  if (isNodeGroupStatus(status)) {
    return NODE_GROUP_STATUS_PRESENTATION[status];
  }
  return UNKNOWN_STATUS_PRESENTATION;
}
