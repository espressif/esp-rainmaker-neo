/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TokenPaginationState } from "@/hooks/use-token-pagination";
import type { OtaImageRow } from "../../ota-images.props";

export interface OtaImagesMainContentProps {
  rows: OtaImageRow[];
  error: Error | null;
  isFetching: boolean;
  pagination: TokenPaginationState;
  hasNextPage: boolean;
  hasPrevPage: boolean;
  /** True while a name-prefix search is applied, so the empty state can explain it. */
  hasActiveSearch: boolean;
  searchTerm: string;
  onNextPage: () => void;
  onPrevPage: () => void;
  onPageSizeChange: (size: number) => void;
}
