/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { SimplifiedDate } from "@espressif/dashboard-ui-components/components";
import type { ResourceCreatedAtProps } from "./resource-created-at.props";

/**
 * "Created <relative date>" meta line for the resource details page headings.
 */
export default function ResourceCreatedAt({ ts }: ResourceCreatedAtProps) {
  const { t } = useTranslation("common");

  if (!ts) {
    return null;
  }

  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
      <span>{t("created", "Created")}</span>
      <SimplifiedDate ts={ts} relative className="text-muted-foreground" />
    </span>
  );
}
