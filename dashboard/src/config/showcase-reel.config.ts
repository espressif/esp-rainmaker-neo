/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Each id maps to `<id>.<locale>.<theme>.{mp4,poster.webp}` in `src/assets/video/showcase/`. The clips are rendered by separate tooling that is not part of this repo.
 */
export interface ShowcaseReelClip {
  id: string;
  /** Fully qualified: the reel renders on several onboarding routes. */
  labelKey: string;
  labelFallback: string;
}

export const SHOWCASE_REEL_CLIPS: readonly ShowcaseReelClip[] = [
  {
    id: "node-management",
    labelKey: "common:showcaseReel.clips.nodeManagement",
    labelFallback: "Generate, register and group nodes",
  },
  {
    id: "ota",
    labelKey: "common:showcaseReel.clips.ota",
    labelFallback: "Upload firmware and roll out an OTA job",
  },
  {
    id: "voice-assistants",
    labelKey: "common:showcaseReel.clips.voiceAssistants",
    labelFallback: "Set up Alexa and Google Home",
  },
];
