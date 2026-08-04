/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useMemo } from "react";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { KeyRound } from "lucide-react";
import { Alert } from "@espressif/dashboard-ui-components/components";
import OnboardingLayout from "@/containers/onboarding/onboarding-layout";
import { OnboardingCard } from "@/components/onboarding-card";
import { BackToSignInLink } from "@/components/back-to-signin-link";
import SetNewPasswordForm from "./_components/set-new-password-form";
import { parseSetPasswordSearch } from "./_schema/set-password-search.schema";

const SET_PASSWORD_PATH = "/set-password";

/**
 * Step 2 of the password reset. The address lives in the query rather than in
 * component state so a reload mid-flow keeps the code the admin already holds
 * usable — Cognito only allows about 5 reset requests per user per hour.
 */
export default function SetPassword() {
  const { t } = useTranslation("set-password");
  const navigate = useNavigate();
  const location = useLocation();

  const { email, sent } = useMemo(
    () => parseSetPasswordSearch(location.search),
    [location.search],
  );

  /**
   * `useLocation` is router-wide, so while a navigation away from this page is
   * in flight the component is still mounted but already reading the *new*
   * location — where `email` is absent. Without this check the guard below
   * would fire mid-transition and redirect every outbound link to step 1.
   */
  const isCurrentRoute = location.pathname === SET_PASSWORD_PATH;

  // A missing or malformed address leaves nothing to submit, so send the admin
  // back to step 1. Redirecting during render is not possible, hence an effect.
  useEffect(() => {
    if (isCurrentRoute && !email) {
      void navigate({ to: "/forgot-password", replace: true });
    }
  }, [isCurrentRoute, email, navigate]);

  const handleSuccess = useCallback(() => {
    void navigate({ to: "/login", search: { reset: "success" } });
  }, [navigate]);

  const handleCodeResent = useCallback(() => {
    void navigate({
      to: SET_PASSWORD_PATH,
      search: { email, sent: true },
      replace: true,
    });
  }, [navigate, email]);

  if (!email) {
    return null;
  }

  return (
    <OnboardingLayout>
      <OnboardingCard
        icon={<KeyRound className="w-6 h-6" />}
        title={t("title", "Set new password")}
        description={t(
          "description",
          "Enter the code you received and choose a new password.",
        )}
        actions={<BackToSignInLink />}
      >
        {sent && (
          <Alert
            title={t("codeSentTitle", "Check your inbox")}
            type="info"
            description={t("codeSent", {
              defaultValue:
                "If {{email}} is registered, a confirmation code has been sent to it.",
              email,
            })}
            hideIcon
            className="mb-4 border-none shadow-none"
          />
        )}

        <SetNewPasswordForm
          email={email}
          onSuccess={handleSuccess}
          onCodeResent={handleCodeResent}
        />
      </OnboardingCard>
    </OnboardingLayout>
  );
}
