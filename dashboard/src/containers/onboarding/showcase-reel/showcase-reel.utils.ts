/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { SupportedLanguage } from "@/lib/constants";

// Globbed here rather than through the shared asset resolver, so the clips ship only in this chunk.
const clipUrls = import.meta.glob<string>("/src/assets/video/showcase/*", {
  eager: true,
  query: "?url",
  import: "default",
});

export interface ShowcaseReelSources {
  mp4: string;
  poster: string;
}

/**
 * Asset URLs for one clip variant: ("ota", "zh", true) -> the `ota.zh.dark.*` files.
 */
export function getShowcaseReelSources(
  clipId: string,
  language: SupportedLanguage,
  darkMode: boolean,
): ShowcaseReelSources {
  const base = `/src/assets/video/showcase/${clipId}.${language}.${darkMode ? "dark" : "light"}`;
  return {
    mp4: clipUrls[`${base}.mp4`] ?? "",
    poster: clipUrls[`${base}.poster.webp`] ?? "",
  };
}
