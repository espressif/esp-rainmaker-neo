/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Fingerprint } from "lucide-react";
import {
  Button,
  MonospaceContent,
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@espressif/dashboard-ui-components/components";
import type { ResourceArnPopoverProps } from "./resource-arn-popover.props";

/**
 * "ARN" action button that reveals the full ARN in a popover. Used by the
 * resource details page headings (node, node group, OTA job).
 */
export default function ResourceArnPopover({ arn }: ResourceArnPopoverProps) {
  const { t } = useTranslation("common");

  if (!arn) {
    return null;
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          startIcon={<Fingerprint className="h-4 w-4" />}
        >
          {t("arn", "ARN")}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-auto p-3">
        <MonospaceContent value={arn} className="text-xs" color="gray" />
      </PopoverContent>
    </Popover>
  );
}
