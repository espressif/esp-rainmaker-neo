/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { BaseSyntheticEvent, FormEventHandler } from "react";

/**
 * Guard the submit handler of a `<form>` that lives inside another form's React
 * tree (rule builders, filter popovers, tag popovers). Radix popovers/dialogs
 * portal out of the DOM, but React still dispatches synthetic events along the
 * React tree, and react-hook-form's `handleSubmit` only calls `preventDefault`
 * — so without this the ancestor page form submits too.
 */
export function isolateNestedFormSubmit(
  handler: (event?: BaseSyntheticEvent) => unknown,
): FormEventHandler<HTMLFormElement> {
  return (event) => {
    event.preventDefault();
    event.stopPropagation();
    void handler(event);
  };
}
