/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { ChevronDown } from "lucide-react";
import { Badge } from "@espressif/dashboard-ui-components/components";
import { QueryRulesPopover } from "@/aws/components/query-rule-builder";
import { isDynamicNodeGroup } from "@/config/node-group-type.config";
import type { GroupTypeBadgeProps } from "./group-type-badge.props";

/**
 * Static/dynamic badge for a node group. Dynamic groups get a chevron that opens
 * the query rules defining their membership; static groups render a plain badge.
 */
export function GroupTypeBadge({ queryString }: GroupTypeBadgeProps) {
  const { t } = useTranslation("node-groups");

  if (!isDynamicNodeGroup(queryString)) {
    return (
      <Badge
        variant="soft"
        className="text-xs font-normal px-2 py-1"
        color="error"
      >
        {t("details.type.static", "Static")}
      </Badge>
    );
  }

  return (
    <QueryRulesPopover
      queryString={queryString}
      trigger={
        <button
          type="button"
          onClick={(e) => e.stopPropagation()}
          aria-label={t(
            "details.type.viewQueryAriaLabel",
            "View dynamic group query rules",
          )}
          className="inline-flex items-center"
        >
          <Badge
            variant="soft"
            className="text-xs font-normal gap-1.5 cursor-pointer px-2 py-1"
            color="success"
          >
            {t("details.type.dynamic", "Dynamic")}
            <ChevronDown className="h-3.5 w-3.5 shrink-0" aria-hidden />
          </Badge>
        </button>
      }
    />
  );
}
