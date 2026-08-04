/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Typography } from "@espressif/dashboard-ui-components/components";
import { SesAccessStatusBadge } from "@/components/ses/ses-access-status-badge";
import type { ValueReadingProps } from "./value-reading.props";

/**
 * Renders one account limit. Discrete states become status badges, consistent with
 * how every other page presents a status.
 *
 * Measurements are plain text, deliberately not `MonospaceContent`: these are prose
 * statements ("50 emails / day per AWS account"), not identifiers, so the monospace
 * font, `tabular-nums` and — most of all — the click-to-copy affordance that
 * component carries would all be misleading here.
 */
export default function ValueReading({ reading }: ValueReadingProps) {
  const { t } = useTranslation("post-deployment");

  if (reading.display === "ses-access") {
    return <SesAccessStatusBadge status={reading.status} />;
  }

  return (
    <Typography variant="body2" as="p" className="font-medium text-foreground">
      {t(reading.i18nKey, {
        defaultValue: reading.defaultValue,
        ...reading.vars,
      })}
    </Typography>
  );
}
