/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ShowcaseReelSources } from "../../showcase-reel.utils";

export interface ShowcaseReelSlideProps {
  sources: ShowcaseReelSources;
  label: string;
  isActive: boolean;
  /** The active slide and the one after it load in full; the rest load metadata only. */
  shouldPreload: boolean;
  isPaused: boolean;
  onEnded: () => void;
}
