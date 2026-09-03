/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Search updater for every in-flow navigation. TanStack Router drops the query
 * string on `navigate()` unless told otherwise, which would lose `?redirect` (the
 * destination a dead session handed over) and `?reset` mid-flow — so every step
 * navigation passes this as its `search`.
 */
export function preserveSearch<TSearch>(previous: TSearch): TSearch {
  return previous;
}
