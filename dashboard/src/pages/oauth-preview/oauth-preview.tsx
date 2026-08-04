/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Alert,
  SimpleCard,
} from "@espressif/dashboard-ui-components/components";
import { clearPreviewRequest, readPreviewRequest } from "@/lib/oauth";

type FailureReason = "rejected" | "stateMismatch" | "noCode";

type PreviewOutcome =
  | { kind: "success" }
  | { kind: "failure"; reason: FailureReason; detail?: string };

const FAILURE_TEXT: Record<
  FailureReason,
  { titleKey: string; title: string; bodyKey: string; body: string }
> = {
  rejected: {
    titleKey: "rejectedTitle",
    title: "Sign-in was not completed",
    bodyKey: "rejectedMessage",
    body: "The sign-in page reported that the request was cancelled or refused.",
  },
  stateMismatch: {
    titleKey: "stateMismatchTitle",
    title: "Response could not be verified",
    bodyKey: "stateMismatchMessage",
    body: "This response does not match the request that started the preview, so it was discarded. Start the preview again from the dashboard.",
  },
  noCode: {
    titleKey: "noCodeTitle",
    title: "No sign-in result returned",
    bodyKey: "noCodeMessage",
    body: "The sign-in page came back without an authorization code, so the login did not finish.",
  },
};

/**
 * Reads the authorization response and decides what it proves. Pure, so the one-shot effect
 * below stays the only place with side effects.
 */
function evaluate(
  params: URLSearchParams,
  expectedState: string | null,
): PreviewOutcome {
  const error = params.get("error");
  if (error) {
    return {
      kind: "failure",
      reason: "rejected",
      detail: params.get("error_description") ?? error,
    };
  }

  // CSRF check: a response carrying a state other than the one we sent is not ours to trust.
  if (!expectedState || params.get("state") !== expectedState) {
    return { kind: "failure", reason: "stateMismatch" };
  }

  if (!params.get("code")) {
    return { kind: "failure", reason: "noCode" };
  }

  return { kind: "success" };
}

/**
 * Where the previewed sign-in lands. It exists to report that an admin's own users can sign in,
 * so it goes no further than the authorization response.
 *
 * The returned code is deliberately never redeemed: the client this preview rides on is
 * confidential, and redeeming its code would mean putting that client's secret in the browser.
 * A code that came back against a matching state already proves the sign-in page rendered and
 * the login completed, which is the whole question being asked.
 *
 * Not under `/home`: whoever lands here arrives mid-redirect in a fresh tab and may hold no
 * dashboard session in it.
 */
export default function OAuthPreview() {
  const { t } = useTranslation("oauth-preview");
  const [outcome, setOutcome] = useState<PreviewOutcome | null>(null);
  const evaluated = useRef(false);

  useEffect(() => {
    if (evaluated.current) {
      return;
    }
    evaluated.current = true;

    const params = new URLSearchParams(window.location.search);
    const stored = readPreviewRequest();
    setOutcome(evaluate(params, stored?.state ?? null));
    // Dropped as soon as it has been read, so nothing can be replayed from a stale session.
    clearPreviewRequest();
  }, []);

  const failure =
    outcome?.kind === "failure" ? FAILURE_TEXT[outcome.reason] : undefined;

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <SimpleCard
        className="w-full max-w-lg"
        title={t("title", "Sign-in preview")}
        description={t(
          "description",
          "The result of signing in through the page your users see.",
        )}
      >
        <div className="flex flex-col gap-3">
          {outcome === null && (
            <Alert
              type="info"
              variant="soft"
              title={t("checking", "Checking the sign-in result…")}
            />
          )}
          {outcome?.kind === "success" && (
            <Alert
              type="success"
              variant="soft"
              title={t("successTitle", "Sign-in completed")}
              description={t(
                "successMessage",
                "The sign-in page worked and the login finished successfully. No tokens were requested — this is a preview, so the authorization code was discarded.",
              )}
            />
          )}
          {failure && outcome?.kind === "failure" && (
            <Alert
              type="error"
              variant="soft"
              title={t(failure.titleKey, failure.title)}
              description={
                outcome.detail ?? t(failure.bodyKey, failure.body)
              }
            />
          )}
          <p className="text-sm text-muted-foreground">
            {t("closeHint", "You can close this tab and return to the dashboard.")}
          </p>
        </div>
      </SimpleCard>
    </div>
  );
}
