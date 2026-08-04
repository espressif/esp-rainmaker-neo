/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useParams } from "@tanstack/react-router";

/**
 * Typed accessor for dynamic route params.
 *
 * Routes are built at runtime from `routesConfig` (see `app/router.tsx`), so
 * TanStack cannot infer param names from a static route tree and
 * `useParams({ strict: false })` degrades to `any`. This hook is the single
 * place that crossing happens: callers declare the params they expect and get a
 * checked object back, instead of scattering `as { … }` assertions across pages.
 *
 * @example
 * const { groupName } = useRouteParams<{ groupName?: string }>();
 */
export function useRouteParams<
  T extends Record<string, string | undefined>,
>(): T {
  /*
   * The one unavoidable assertion in the app: `useParams` is untyped under
   * runtime route construction, so this is where `any` becomes `T`. Keeping it
   * here means no page needs its own assertion — or its own disable comment.
   *
   * Landing in `unknown` first is deliberate: letting `T` reach `useParams`
   * directly makes TypeScript try to infer its `TSelected` generic from `T`,
   * which fails against the hook's `structuralSharing` option type.
   */
  const params: unknown = useParams({ strict: false });
  return params as T;
}
