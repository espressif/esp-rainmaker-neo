/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ArrowRight, Image, Languages, Palette, Shapes } from "lucide-react";
import {
  SectionCard,
  Tabs,
  TabsContent,
  TabsContents,
  TabsList,
  TabsTrigger,
} from "@espressif/dashboard-ui-components/components";
import type { BrandMode } from "@/utils/rainmaker-brand/rainmaker-brand";
import {
  BRAND_CARD_PROPS,
  BRAND_MODE_LABELS,
  TRANSPARENCY_GRID_STYLE,
} from "../../branding.constants";
import { getBrandPreview, getLogoFacts } from "../../branding.utils";
import type { BrandPreviewProps } from "./brand-preview.props";

const SECTION_ICON_CLASS = "h-5 w-5";
const IMAGE_TILE_CLASS =
  "flex items-center justify-center overflow-hidden border border-border";
const ITEM_CLASS = "rounded-xl border border-border";

/** Read-only view of what the brand changes in the app; logos and icons sit on the brand's own background, so they read the way the phone shows them in either dashboard theme. */
export default function BrandPreview({ brand }: BrandPreviewProps) {
  const { t } = useTranslation("branding");
  const { config } = brand;
  const preview = useMemo(() => getBrandPreview(brand), [brand]);
  const modeLabel = (mode: BrandMode) =>
    t(BRAND_MODE_LABELS[mode].i18nKey, BRAND_MODE_LABELS[mode].fallback);

  return (
    <>
      <SectionCard
        {...BRAND_CARD_PROPS}
        icon={<Image className={SECTION_ICON_CLASS} aria-hidden />}
        primaryText={t("logo.heading", "Logo")}
        secondaryText={t(
          "logo.secondaryText",
          "The logo on the app's background in each mode it ships.",
        )}
      >
        <div className="flex flex-col gap-6">
          <div className="grid gap-4 sm:grid-cols-2">
            {preview.logos.map(({ mode, src, style }) => (
              <figure key={mode} className="flex flex-col gap-2">
                <div
                  className={`${IMAGE_TILE_CLASS} h-40 rounded-2xl p-4`}
                  style={style}
                >
                  {src && (
                    <img
                      src={src}
                      alt={t("logo.alt", "{{appName}} logo in {{mode}}", {
                        appName: config.appName,
                        mode: modeLabel(mode),
                      })}
                      className="max-h-full max-w-full object-contain"
                    />
                  )}
                </div>
                <figcaption className="text-xs text-muted-foreground">
                  {modeLabel(mode)}
                </figcaption>
              </figure>
            ))}
          </div>
          <dl className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            {getLogoFacts(config, t).map((fact) => (
              <div
                key={fact.label}
                className={`${ITEM_CLASS} flex flex-col gap-1 px-4 py-3`}
              >
                <dt className="text-xs text-muted-foreground">{fact.label}</dt>
                <dd className="text-sm font-medium text-foreground">
                  {fact.value}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </SectionCard>

      {preview.palettes.length > 0 && (
        <SectionCard
          {...BRAND_CARD_PROPS}
          icon={<Palette className={SECTION_ICON_CLASS} aria-hidden />}
          primaryText={t("colors.heading", "Colours")}
          secondaryText={t(
            "colors.secondaryText",
            "The palette the app uses in place of its default colours, per mode.",
          )}
        >
          <Tabs defaultValue={preview.palettes[0].mode}>
            <TabsList variant="line">
              {preview.palettes.map(({ mode }) => (
                <TabsTrigger key={mode} value={mode}>
                  {modeLabel(mode)}
                </TabsTrigger>
              ))}
            </TabsList>
            <TabsContents className="pt-4">
              {preview.palettes.map(({ mode, swatches }) => (
                <TabsContent key={mode} value={mode}>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
                    {swatches.map(({ role, value, style }) => (
                      <div
                        key={role}
                        className={`${ITEM_CLASS} flex min-w-0 items-center gap-3 p-2`}
                      >
                        <div
                          className="h-10 w-10 shrink-0 overflow-hidden rounded-lg ring-1 ring-border"
                          style={TRANSPARENCY_GRID_STYLE}
                        >
                          <div className="h-full w-full" style={style} />
                        </div>
                        <div className="min-w-0">
                          <p className="truncate text-sm font-medium text-foreground">
                            {role}
                          </p>
                          <p
                            className="truncate font-mono text-xs text-muted-foreground"
                            title={value}
                          >
                            {value}
                          </p>
                        </div>
                      </div>
                    ))}
                  </div>
                </TabsContent>
              ))}
            </TabsContents>
          </Tabs>
        </SectionCard>
      )}

      {preview.icons.length > 0 && (
        <SectionCard
          {...BRAND_CARD_PROPS}
          icon={<Shapes className={SECTION_ICON_CLASS} aria-hidden />}
          primaryText={t("icons.heading", "Icons")}
          secondaryText={t(
            "icons.secondaryText",
            "Device and screen icons that replace the app's own.",
          )}
        >
          <ul className="grid grid-cols-2 gap-3 sm:grid-cols-4 xl:grid-cols-6">
            {preview.icons.map(([name, url]) => (
              <li
                key={name}
                className={`${ITEM_CLASS} flex min-w-0 flex-col items-center gap-2 p-3`}
              >
                <div
                  className={`${IMAGE_TILE_CLASS} h-14 w-14 rounded-xl p-2`}
                  style={preview.iconStyle}
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
        </SectionCard>
      )}

      {preview.words.length > 0 && (
        <SectionCard
          {...BRAND_CARD_PROPS}
          icon={<Languages className={SECTION_ICON_CLASS} aria-hidden />}
          primaryText={t("wording.heading", "Wording")}
          secondaryText={t(
            "wording.secondaryText",
            "Words the app uses in place of its own.",
          )}
        >
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {preview.words.map(([word, replacement]) => (
              <div
                key={word}
                className={`${ITEM_CLASS} flex min-w-0 items-center gap-3 px-4 py-3`}
              >
                <dt className="shrink-0 font-mono text-xs text-muted-foreground">
                  {word}
                </dt>
                <ArrowRight
                  className="h-4 w-4 shrink-0 text-muted-foreground"
                  aria-hidden
                />
                <dd className="min-w-0 truncate text-sm font-medium text-foreground">
                  {replacement}
                </dd>
              </div>
            ))}
          </dl>
        </SectionCard>
      )}
    </>
  );
}
