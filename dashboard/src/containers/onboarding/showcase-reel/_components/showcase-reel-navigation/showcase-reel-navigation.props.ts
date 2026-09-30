/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { CarouselNavigationState } from "@espressif/dashboard-ui-components/components";

export interface ShowcaseReelNavigationProps {
  state: CarouselNavigationState;
  slideLabels: readonly string[];
  isPaused: boolean;
  onTogglePause: () => void;
  /** Reports the carousel's position and its `scrollNext`, which the slides need for `onEnded`. */
  onSync: (selectedIndex: number, scrollNext: () => void) => void;
}
