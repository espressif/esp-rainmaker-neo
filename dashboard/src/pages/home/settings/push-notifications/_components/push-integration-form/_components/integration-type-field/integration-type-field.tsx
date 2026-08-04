/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Controller, useFormContext } from "react-hook-form";
import { useTranslation } from "react-i18next";
import {
  Badge,
  SelectableCardList,
  type SelectableCardListItem,
  Typography
} from "@espressif/dashboard-ui-components/components";
import { CustomIcon } from "@/components/custom-icon";
import { PUSH_INTEGRATION_TYPE_OPTIONS } from "@/config/push-integration.config";
import type { PushIntegrationFormValues } from "../../push-integration-form.schema";

/** Single-select platform picker (iOS / Android) with a solid platform badge. */
export default function IntegrationTypeField() {
  const { t } = useTranslation("push-notifications");
  const { control } = useFormContext<PushIntegrationFormValues>();

  const items: SelectableCardListItem[] = PUSH_INTEGRATION_TYPE_OPTIONS.map(
    (option) => ({
      id: option.id,
      // `text-foreground` pins the mark to its brand colour: the Apple icon is
      // drawn with `currentColor`, so without this it picks up the selected
      // card's primary tint while the multi-colour Android mark stays put.
      icon: (
        <CustomIcon
          type={option.iconType}
          size={20}
          className="text-foreground"
        />
      ),
      ariaLabel: t(option.primaryKey),
      primaryText: (
        <div className="flex items-center gap-2">
          <Typography variant="h6" className="flex-1">
            {t(option.primaryKey)}
          </Typography>
          <Badge
            variant="gradient"
            color="info"
            className="border border-solid border-info/20"
          >
            {t(option.badgeKey)}
          </Badge>
        </div>
      ),
    }),
  );

  return (
    <Controller
      control={control}
      name="integration_type"
      render={({ field }) => (
        <SelectableCardList
          aria-label={t("form.typeLabel", "Integration type")}
          data={items}
          value={field.value}
          onChange={field.onChange}
          allowMultiple={false}
        />
      )}
    />
  );
}
