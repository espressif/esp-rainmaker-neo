/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { JobStatus } from "@aws-sdk/client-iot";
import { Check, ChevronDown, Circle } from "lucide-react";
import {
  Button,
  List,
  Menu,
} from "@espressif/dashboard-ui-components/components";
import type { ListGroup } from "@espressif/dashboard-ui-components/components";
import { cn } from "@/utils/utils";
import { getPresetColorTextClass } from "@/config/node-status.config";
import {
  getOtaJobStatusPresentation,
  OTA_JOB_STATUS_FILTER_IDS,
} from "@/config/ota-job-status.config";
import type { OtaJobStatusFilterProps } from "./ota-job-status-filter.props";

/**
 * Statuses AWS returns that we have no translation for carry no `i18nKey`; those
 * render the raw status rather than asking i18next to resolve an empty key.
 */
function statusLabel(
  i18nKey: string | undefined,
  status: string,
  t: TFunction,
): string {
  return i18nKey ? t(i18nKey, status) : status;
}

export function OtaJobStatusFilter({ value, onChange }: OtaJobStatusFilterProps) {
  const { t } = useTranslation(["ota-jobs", "common"]);

  const selectedPresentation = getOtaJobStatusPresentation(value ?? undefined);
  const selectedLabel = value
    ? statusLabel(selectedPresentation.i18nKey, value, t)
    : t("common:columns.status", "Status");

  const handleSelect = useCallback(
    (status: JobStatus) => {
      onChange(status === value ? null : status);
    },
    [onChange, value],
  );

  const statusListItems: ListGroup[] = useMemo(
    () => [
      {
        id: "ota-job-status-options",
        items: OTA_JOB_STATUS_FILTER_IDS.map((status) => {
          const { color, i18nKey } = getOtaJobStatusPresentation(status);
          const isSelected = status === value;
          return {
            id: status,
            label: statusLabel(i18nKey, status, t),
            startIcon: (
              <Circle
                className={cn(
                  "h-3.5 w-3.5 shrink-0",
                  getPresetColorTextClass(color),
                )}
                fill="currentColor"
                stroke="none"
              />
            ),
            endIcon: (
              <Check
                className={cn(
                  "h-4 w-4 shrink-0",
                  isSelected ? "opacity-100" : "opacity-0",
                )}
                aria-hidden
              />
            ),
            isSelected,
            onClick: () => handleSelect(status),
          };
        }),
      },
    ],
    [t, value, handleSelect],
  );

  return (
    <Menu
      trigger={
        <Button
          variant="outline"
          color="gray"
          size="sm"
          usePrimaryColorOnHover
          fullWidth={false}
          startIcon={
            <Circle
              className={cn(
                "h-3.5 w-3.5 shrink-0",
                value
                  ? getPresetColorTextClass(selectedPresentation.color)
                  : "text-muted-foreground",
              )}
              fill="currentColor"
              stroke="none"
            />
          }
          endIcon={<ChevronDown />}
        >
          {selectedLabel}
        </Button>
      }
      align="start"
    >
      <List
        items={statusListItems}
        role="dropdown-list"
        showSeparators={false}
      />
    </Menu>
  );
}
