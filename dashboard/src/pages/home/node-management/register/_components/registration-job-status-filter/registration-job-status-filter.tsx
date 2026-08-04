/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
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
  getRegistrationJobStatusPresentation,
  REGISTRATION_JOB_STATUS_IDS,
  type RegistrationJobStatus,
} from "@/config/registration-job-status.config";
import type { RegistrationJobStatusFilterProps } from "./registration-job-status-filter.props";

export function RegistrationJobStatusFilter({
  value,
  onChange,
}: RegistrationJobStatusFilterProps) {
  const { t } = useTranslation(["register", "common"]);

  const selectedPresentation = getRegistrationJobStatusPresentation(value);
  const selectedLabel = value
    ? t(selectedPresentation.i18nKey, selectedPresentation.labelFallback)
    : t("common:columns.status", "Status");

  const handleSelect = useCallback(
    (status: RegistrationJobStatus) => {
      onChange(status === value ? null : status);
    },
    [onChange, value],
  );

  const statusListItems: ListGroup[] = useMemo(
    () => [
      {
        id: "registration-job-status-options",
        items: REGISTRATION_JOB_STATUS_IDS.map((status) => {
          const { color, i18nKey, labelFallback } =
            getRegistrationJobStatusPresentation(status);
          const isSelected = status === value;
          return {
            id: status,
            label: t(i18nKey, labelFallback),
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
