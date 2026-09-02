/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useLocation } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { LogIn } from "lucide-react";
import OnboardingLayout from "@/containers/onboarding/onboarding-layout";
import { OnboardingCard } from "@/components/onboarding-card";
import { BackToSignInLink } from "@/components/back-to-signin-link";
import { getLastLoginEmail, otpErrorMessage } from "@/lib/auth";
import { useSigninFlowStore } from "@/stores/signin-flow.store";
import { parseLoginSearch } from "../_schema/login-search.schema";
import { resolveErrorMessage } from "../_utils/signin-errors";
import { useSigninFlow } from "../_hooks/use-signin-flow";
import { SigninAlerts } from "../_components/signin-alerts";
import { EmailForm } from "./_components/email-form";

/**
 * Screen 3 — email entry. The flow's cold start for a browser with no
 * remembered account, and the "choose another email" target for one that has.
 */
export default function LoginEmail() {
  const { t } = useTranslation(["login", "common"]);
  const location = useLocation();
  const flow = useSigninFlow();
  const username = useSigninFlowStore((s) => s.username);
  const flowMessage = useSigninFlowStore((s) => s.flowMessage);
  const loginSearch = useMemo(
    () => parseLoginSearch(location.search),
    [location.search],
  );

  // Back only leads somewhere when a remembered account exists; without one,
  // this screen *is* the entry, and a dead-end Back would just confuse.
  const hasRememberedAccount = Boolean(getLastLoginEmail());

  const errorMessage = resolveErrorMessage(
    t,
    null,
    otpErrorMessage(flow.error) ?? flowMessage,
    flow.error,
  );

  return (
    <OnboardingLayout>
      <OnboardingCard
        icon={<LogIn className="w-6 h-6" />}
        title={t("title", "Sign in")}
        description={t(
          "description",
          "Enter your credentials to access your account",
        )}
        actions={hasRememberedAccount ? <BackToSignInLink to="/login" /> : undefined}
      >
        <SigninAlerts
          errorMessage={errorMessage}
          resetOutcome={loginSearch.reset}
        />

        <EmailForm
          defaultUsername={username}
          isSubmitting={flow.isStarting || flow.isSending}
          onSubmit={flow.identify}
        />
      </OnboardingCard>
    </OnboardingLayout>
  );
}
