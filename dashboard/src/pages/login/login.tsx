/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useMemo } from "react";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowRightIcon, LogIn } from "lucide-react";
import OnboardingLayout from "@/containers/onboarding/onboarding-layout";
import { Button } from "@espressif/dashboard-ui-components/components";
import { OnboardingCard } from "@/components/onboarding-card";
import { getLastLoginEmail, otpErrorMessage } from "@/lib/auth";
import { useSigninFlowStore } from "@/stores/signin-flow.store";
import { parseLoginSearch } from "./_schema/login-search.schema";
import { resolveErrorMessage } from "./_utils/signin-errors";
import { preserveSearch } from "./_utils/preserve-search";
import { useSigninFlow } from "./_hooks/use-signin-flow";
import { AccountChip } from "./_components/account-chip";
import { SigninAlerts } from "./_components/signin-alerts";

/**
 * Screen 1 — the remembered account. Greets the admin who has signed in on this
 * browser before; everyone else is sent to the email form. Continue kicks off
 * identification for the saved address exactly as if it had been typed.
 */
export default function Login() {
  const { t } = useTranslation(["login", "common"]);
  const navigate = useNavigate();
  const location = useLocation();
  const rememberedEmail = getLastLoginEmail();
  const flow = useSigninFlow();
  const flowMessage = useSigninFlowStore((s) => s.flowMessage);
  const loginSearch = useMemo(
    () => parseLoginSearch(location.search),
    [location.search],
  );

  // The entry-route rule applied to the entry itself: nothing to remember means
  // this screen has no reason to exist, so the email form is the entry.
  useEffect(() => {
    if (!rememberedEmail) {
      void navigate({
        to: "/login/email",
        search: preserveSearch,
        replace: true,
      });
    }
  }, [rememberedEmail, navigate]);

  if (!rememberedEmail) {
    return null;
  }

  const errorMessage = resolveErrorMessage(
    t,
    null,
    otpErrorMessage(flow.error) ?? flowMessage,
    flow.error,
  );

  const isContinuing = flow.isStarting || flow.isSending;
  // Shared by the Continue button and the account card. The pending guard is
  // for the card: unlike the button, it has no loading state to swallow a
  // second click, and a double `identify` would fire two Cognito calls.
  const continueAsRememberedAccount = () => {
    if (!isContinuing) {
      flow.identify(rememberedEmail);
    }
  };

  return (
    <OnboardingLayout>
      <OnboardingCard
        icon={<LogIn className="w-6 h-6" />}
        title={t("welcomeBack", "Welcome back")}
        description={t(
          "welcomeBackDescription",
          "Continue with the account you last signed in with.",
        )}
      >
        <SigninAlerts
          errorMessage={errorMessage}
          resetOutcome={loginSearch.reset}
        />

        <div className="space-y-3">
          <AccountChip
            email={rememberedEmail}
            onClick={continueAsRememberedAccount}
          />

          <div className="flex flex-col gap-3">
            <Button
              type="button"
              size="lg"
              loading={isContinuing}
              loadingIndicator="progress-bar"
              endIcon={<ArrowRightIcon className="w-4 h-4" />}
              animateEndIconOnHover={true}
              onClick={continueAsRememberedAccount}
            >
              {t("continue", "Continue")}
            </Button>

            <Button
              type="button"
              variant="outline"
              size="lg"
              onClick={() =>
                void navigate({ to: "/login/email", search: preserveSearch })
              }
            >
              {t("chooseAnotherEmail", "Choose another email")}
            </Button>
          </div>
        </div>
      </OnboardingCard>
    </OnboardingLayout>
  );
}
