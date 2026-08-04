/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { BaseSyntheticEvent, FormEventHandler } from "react";

/**
 * Adapt react-hook-form's `handleSubmit` result to a void-returning submit
 * handler.
 *
 * `form.handleSubmit(fn)` returns `(event?) => Promise<void>`. Handing that
 * straight to `<form onSubmit>` puts a promise in a void-typed slot: React never
 * awaits it, so a rejection escaping `fn` disappears without surfacing anywhere.
 * Wrapping keeps rejection handling where it belongs — inside `fn` — and keeps
 * the JSX honest about returning nothing.
 *
 * For a form nested inside another form's React tree, use
 * [`isolateNestedFormSubmit`](./isolate-nested-form-submit.ts) instead — it
 * voids the promise too, and additionally stops the event from reaching the
 * ancestor form.
 */
export function voidFormSubmit(
  handler: (event?: BaseSyntheticEvent) => unknown,
): FormEventHandler<HTMLFormElement> {
  return (event) => {
    void handler(event);
  };
}
