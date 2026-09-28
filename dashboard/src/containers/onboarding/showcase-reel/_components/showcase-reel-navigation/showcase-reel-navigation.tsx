/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Pause, Play } from "lucide-react";
import { Button } from "@espressif/dashboard-ui-components/components";
import { cn } from "@/utils/utils";
import type { ShowcaseReelNavigationProps } from "./showcase-reel-navigation.props";

export default function ShowcaseReelNavigation({
  state,
  slideLabels,
  isPaused,
  onTogglePause,
  onSync,
}: ShowcaseReelNavigationProps) {
  const { t } = useTranslation("common");
  const { selectedIndex, scrollNext, scrollTo } = state;

  useEffect(() => {
    onSync(selectedIndex, scrollNext);
  }, [onSync, selectedIndex, scrollNext]);

  const pauseLabel = isPaused
    ? t("showcaseReel.play", "Play showcase")
    : t("showcaseReel.pause", "Pause showcase");

  return (
    <div className="absolute inset-x-0 bottom-6 flex items-center justify-center gap-3">
      <div
        className="flex items-center gap-1.5"
        role="tablist"
        aria-label={t("showcaseReel.pagination", "Showcase slides")}
      >
        {slideLabels.map((label, index) => (
          <button
            key={label}
            type="button"
            role="tab"
            aria-selected={index === selectedIndex}
            aria-label={label}
            onClick={() => scrollTo(index)}
            className={cn(
              "h-1.5 rounded-full transition-all duration-200 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              index === selectedIndex
                ? "w-5 bg-foreground"
                : "w-1.5 bg-muted-foreground/40 hover:bg-muted-foreground/60",
            )}
          />
        ))}
      </div>
      <Button
        variant="ghost"
        size="icon"
        className="h-7 w-7 text-muted-foreground"
        aria-label={pauseLabel}
        tooltip={pauseLabel}
        onClick={onTogglePause}
      >
        {isPaused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
      </Button>
    </div>
  );
}
