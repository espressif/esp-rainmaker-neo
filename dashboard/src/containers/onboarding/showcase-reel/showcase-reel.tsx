/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useMedia } from "react-use";
import {
  Carousel,
  type CarouselItem,
  type CarouselNavigationState,
} from "@espressif/dashboard-ui-components/components";
import { SHOWCASE_REEL_CLIPS } from "@/config/showcase-reel.config";
import { useAppStore } from "@/stores/app.store";
import ShowcaseReelSlide from "./_components/showcase-reel-slide/showcase-reel-slide";
import ShowcaseReelNavigation from "./_components/showcase-reel-navigation/showcase-reel-navigation";
import { getShowcaseReelSources } from "./showcase-reel.utils";
import type { ShowcaseReelProps } from "./showcase-reel.props";

export default function ShowcaseReel({ darkMode }: ShowcaseReelProps) {
  const { t } = useTranslation("common");
  const language = useAppStore((state) => state.language);
  const prefersReducedMotion = useMedia("(prefers-reduced-motion: reduce)");
  const [isPaused, setIsPaused] = useState(prefersReducedMotion);
  const [activeIndex, setActiveIndex] = useState(0);
  const scrollNextRef = useRef<() => void>(() => undefined);

  const slideLabels = useMemo(
    () => SHOWCASE_REEL_CLIPS.map((clip) => t(clip.labelKey, clip.labelFallback)),
    [t],
  );

  const handleEnded = useCallback(() => scrollNextRef.current(), []);
  const handleTogglePause = useCallback(() => setIsPaused((paused) => !paused), []);
  const handleSync = useCallback((selectedIndex: number, scrollNext: () => void) => {
    setActiveIndex(selectedIndex);
    scrollNextRef.current = scrollNext;
  }, []);

  const slides = useMemo<CarouselItem[]>(
    () =>
      SHOWCASE_REEL_CLIPS.map((clip, index) => ({
        id: clip.id,
        content: (
          <ShowcaseReelSlide
            sources={getShowcaseReelSources(clip.id, language, darkMode)}
            label={slideLabels[index]}
            isActive={index === activeIndex}
            shouldPreload={
              index === activeIndex || index === (activeIndex + 1) % SHOWCASE_REEL_CLIPS.length
            }
            isPaused={isPaused}
            onEnded={handleEnded}
          />
        ),
      })),
    [language, darkMode, slideLabels, activeIndex, isPaused, handleEnded],
  );

  const renderNavigation = useCallback(
    (state: CarouselNavigationState) => (
      <ShowcaseReelNavigation
        state={state}
        slideLabels={slideLabels}
        isPaused={isPaused}
        onTogglePause={handleTogglePause}
        onSync={handleSync}
      />
    ),
    [slideLabels, isPaused, handleTogglePause, handleSync],
  );

  return (
    <Carousel
      data={slides}
      navigation="custom"
      renderNavigation={renderNavigation}
      className="relative h-full overflow-hidden bg-background"
    />
  );
}
