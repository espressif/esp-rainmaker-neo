/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/api/api.errors";
import { openPreviewSignIn } from "@/lib/oauth";

/**
 * Drives the "Preview sign-in page" action. Registering this origin's callback is a write
 * against the client registry, so it can fail on permissions or on a client the deployment
 * never seeded — the caller gets a message to show rather than a silently dead button.
 */
export function usePreviewSignIn() {
  const { t } = useTranslation("voice-assistants");
  const [isPreparing, setIsPreparing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  /**
   * Latched: the card it lives on is collapsed by default, so a failure has to pull the body
   * open. Never cleared, otherwise retrying would collapse a card the admin has since expanded.
   */
  const [hasFailed, setHasFailed] = useState(false);

  const start = useCallback(async () => {
    setError(null);
    setIsPreparing(true);
    try {
      await openPreviewSignIn();
    } catch (caught) {
      setHasFailed(true);
      // The registry is superadmin-only, which is the one failure a plain admin will hit.
      if (ApiError.isApiError(caught) && caught.isAuthError()) {
        setError(
          t(
            "previewSignInForbidden",
            "Registering the preview callback needs superadmin access.",
          ),
        );
      } else {
        setError(
          caught instanceof Error && caught.message
            ? caught.message
            : t(
                "previewSignInFailedMessage",
                "The sign-in page could not be prepared. Try again.",
              ),
        );
      }
    } finally {
      setIsPreparing(false);
    }
  }, [t]);

  return { start, isPreparing, error, hasFailed };
}
