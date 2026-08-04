/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { KeyRound } from "lucide-react";
import OnboardingLayout from "@/containers/onboarding/onboarding-layout";
import { OnboardingCard } from "@/components/onboarding-card";
import { BackToSignInLink } from "@/components/back-to-signin-link";
import RequestCodeForm from "./_components/request-code-form";

/**
 * Step 1 of the password reset: collect the address and mail a code. The code
 * is redeemed on `/set-password`, which carries the address in its query so
 * the flow survives a reload.
 */
export default function ForgotPassword() {
  const { t } = useTranslation("forgot-password");
  const navigate = useNavigate();

  const goToSetPassword = useCallback(
    (email: string, codeJustSent: boolean) => {
      void navigate({
        to: "/set-password",
        search: codeJustSent ? { email, sent: true } : { email },
      });
    },
    [navigate],
  );

  const handleCodeSent = useCallback(
    (email: string) => goToSetPassword(email, true),
    [goToSetPassword],
  );

  const handleHasCode = useCallback(
    (email: string) => goToSetPassword(email, false),
    [goToSetPassword],
  );

  return (
    <OnboardingLayout>
      <OnboardingCard
        icon={<KeyRound className="w-6 h-6" />}
        title={t("title", "Reset password")}
        description={t(
          "description",
          "Enter the email address of your admin account and we will send you a confirmation code.",
        )}
        actions={<BackToSignInLink />}
      >
        <RequestCodeForm
          onCodeSent={handleCodeSent}
          onHasCode={handleHasCode}
        />
      </OnboardingCard>
    </OnboardingLayout>
  );
}
