/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Gets a URL parameter value by name
 * @param paramName - The name of the URL parameter
 * @returns The parameter value if present, undefined otherwise
 */
export function getURLParamValue(paramName: string): string | undefined {
  if (typeof window === "undefined") {return undefined;}
  const params = new URLSearchParams(window.location.search);
  return params.get(paramName) || undefined;
}

export function trimOrUndefined(value: string | null | undefined): string | undefined {
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed === "" ? undefined : trimmed;
}

/** Like {@link trimOrUndefined}, but returns `null` for the missing case. */
export function trimOrNull(value: string | null | undefined): string | null {
  return trimOrUndefined(value) ?? null;
}

