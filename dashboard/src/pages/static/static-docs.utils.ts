/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { SupportedLanguage } from "@/lib/constants";
import { getStaticDocById, type StaticDocId } from "./static-docs.config";

/**
 * Every document is inlined as a string at build time. Keys match the `files` values in
 * `static-docs.config.ts` (e.g. `./docs/en/terms-of-use.md`).
 *
 * Eager rather than lazy: this module is only reachable from the lazily-loaded `/static`
 * chunk, so nothing lands in the main bundle, and resolution stays a synchronous lookup —
 * the pages need no loading or error states for content that ships with the app.
 */
const docContents = import.meta.glob<string>("./docs/**/*.md", {
  query: "?raw",
  import: "default",
  eager: true,
});

/**
 * Locale the documents are authored in, and therefore the one guaranteed to exist.
 * Intentionally a content fact rather than `appConfig.defaults.language`: changing the
 * app's default language must not repoint the fallback at an unwritten translation.
 */
const FALLBACK_LANGUAGE: SupportedLanguage = "en";

export type ResolvedStaticDoc = {
  content: string;
  /** Locale actually rendered — differs from the requested one when falling back. */
  locale: SupportedLanguage;
};

/**
 * Resolves a document's markdown for a locale, falling back to English.
 *
 * The glob map is checked rather than just the config, so an entry pointing at a file that
 * has been moved or deleted degrades to the fallback instead of rendering nothing.
 */
export function resolveStaticDoc(
  docId: StaticDocId,
  language: SupportedLanguage,
): ResolvedStaticDoc | null {
  const doc = getStaticDocById(docId);
  if (!doc) {
    return null;
  }

  const requestedFile = doc.files[language];
  if (requestedFile && docContents[requestedFile]) {
    return { content: docContents[requestedFile], locale: language };
  }

  const fallbackFile = doc.files[FALLBACK_LANGUAGE];
  if (fallbackFile && docContents[fallbackFile]) {
    return { content: docContents[fallbackFile], locale: FALLBACK_LANGUAGE };
  }

  return null;
}
