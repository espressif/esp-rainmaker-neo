/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Shapes } from "lucide-react";
import { SectionCard } from "@espressif/dashboard-ui-components/components";
import {
  BRAND_CARD_PROPS,
  BRAND_IMAGE_TILE_CLASS,
  BRAND_ITEM_CLASS,
} from "../../branding.constants";
import type { BrandIconsProps } from "./brand-icons.props";

export default function BrandIcons({ groups, iconStyle }: BrandIconsProps) {
  const { t } = useTranslation("branding");

  return (
    <SectionCard
      {...BRAND_CARD_PROPS}
      icon={<Shapes className="h-5 w-5" aria-hidden />}
      primaryText={t("icons.heading", "Icons")}
      secondaryText={t(
        "icons.secondaryText",
        "Device icons, AI agent icons and screen illustrations that replace the app's own.",
      )}
    >
      <div className="flex flex-col gap-5">
        {groups.map(({ group, label, icons }) => (
          <section key={group} className="flex flex-col gap-3">
            <h3 className="text-sm font-medium text-foreground">
              {label ? t(label.i18nKey, label.fallback) : group}
            </h3>
            <ul className="grid grid-cols-2 gap-3 sm:grid-cols-4 xl:grid-cols-6">
              {icons.map(([name, url]) => (
                <li
                  key={name}
                  className={`${BRAND_ITEM_CLASS} flex min-w-0 flex-col items-center gap-2 p-3`}
                >
                  <div
                    className={`${BRAND_IMAGE_TILE_CLASS} h-14 w-14 rounded-xl p-2`}
                    style={iconStyle}
                  >
                    <img
                      src={url}
                      alt={t("icons.alt", "{{name}} icon", { name })}
                      className="max-h-full max-w-full object-contain"
                      loading="lazy"
                    />
                  </div>
                  <span className="w-full truncate text-center font-mono text-xs text-muted-foreground">
                    {name}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </SectionCard>
  );
}
