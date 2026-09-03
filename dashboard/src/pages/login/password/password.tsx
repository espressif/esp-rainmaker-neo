/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { KeyRound, MailIcon } from "lucide-react";
import OnboardingLayout from "@/containers/onboarding/onboarding-layout";
import { Button } from "@espressif/dashboard-ui-components/components";
import { OnboardingCard } from "@/components/onboarding-card";
import { BackToSignInLink } from "@/components/back-to-signin-link";
import { appConfig } from "@/lib/app-config";
import { useForgotPassword, useSignin } from "@/api";
import { useSigninFlowStore } from "@/stores/signin-flow.store";
import { resolveErrorMessage } from "../_utils/signin-errors";
import { useSigninFlow } from "../_hooks/use-signin-flow";
import { useCompleteSignin } from "../_hooks/use-complete-signin";
import {
  signinEntryRoute,
  useSigninStepGuard,
} from "../_hooks/use-signin-step-guard";
import { SigninAlerts } from "../_components/signin-alerts";
import { AccountChip } from "../_components/account-chip";
import { PasswordForm } from "./_components/password-form";

/**
 * Screen 4 — password entry. The address is settled by now (account chip, not a
 * field); only the password is asked for. Error state and mutations are local
 * to this page, so leaving it drops them by construction.
 */
export default function LoginPassword() {
  const { t } = useTranslation(["login", "common"]);
  const navigate = useNavigate();
  const flow = useSigninFlow();
  const completeSignin = useCompleteSignin();
  const signinMutation = useSignin();
  const forgotMutation = useForgotPassword();
  const [loginError, setLoginError] = useState<string | null>(null);
  const username = useSigninFlowStore((s) => s.username);
  const challenges = useSigninFlowStore((s) => s.challenges);
  const keepSignedIn = useSigninFlowStore((s) => s.keepSignedIn);
  const setKeepSignedIn = useSigninFlowStore((s) => s.setKeepSignedIn);
  const flowMessage = useSigninFlowStore((s) => s.flowMessage);

  const guardSatisfied = useSigninStepGuard("username");
  if (!guardSatisfied) {
    return null;
  }

  const allowKeepMeSignedIn =
    appConfig.customAuth?.allowKeepMeSignedIn ?? false;
  const canUseOtp = challenges.includes("EMAIL_OTP");

  const onPasswordSubmit = (password: string) => {
    setLoginError(null);
    signinMutation.mutate(
      { username, password },
      {
        onSuccess: (response) => {
          // Backend returns 200 with only token_type for invalid credentials
          if (!response.access_token || !response.id_token) {
            signinMutation.reset();
            setLoginError(
              t("invalidCredentials", "Invalid username or password"),
            );
            return;
          }
          completeSignin(username, response);
        },
      },
    );
  };

  // The reset code is auto-requested for the known address, so Screen 5 is one
  // click away — no detour through the standalone request form.
  const onForgotPassword = () => {
    setLoginError(null);
    forgotMutation.mutate(
      { username },
      {
        onSuccess: () => {
          void navigate({
            to: "/set-password",
            search: { email: username, sent: true },
          });
        },
      },
    );
  };

  // A code was already mailed on the way in whenever EMAIL_OTP is available, so
  // Back can return to the code screen without triggering a new send.
  const backTo = canUseOtp ? "/login/otp" : signinEntryRoute();

  const errorMessage = resolveErrorMessage(
    t,
    loginError,
    flowMessage,
    signinMutation.error ?? forgotMutation.error,
  );

  return (
    <OnboardingLayout>
      <OnboardingCard
        icon={<KeyRound className="w-6 h-6" />}
        title={t("password.title", "Enter your password")}
        description={t(
          "password.description",
          "Sign in with the password for the account below.",
        )}
        actions={<BackToSignInLink to={backTo} />}
      >
        <SigninAlerts errorMessage={errorMessage} />

        <div className="space-y-6">
          <AccountChip email={username} />

          <div className="flex flex-col gap-3">
            <PasswordForm
              allowKeepMeSignedIn={allowKeepMeSignedIn}
              keepSignedIn={keepSignedIn}
              onKeepSignedInChange={setKeepSignedIn}
              isSubmitting={signinMutation.isPending}
              isRequestingReset={forgotMutation.isPending}
              onSubmit={onPasswordSubmit}
              onForgotPassword={onForgotPassword}
            />

            {canUseOtp && (
              <Button
                type="button"
                variant="outline"
                size="lg"
                startIcon={<MailIcon className="w-4 h-4" />}
                onClick={flow.chooseOtp}
              >
                {t("password.emailCode", "Email me a code instead")}
              </Button>
            )}
          </div>
        </div>
      </OnboardingCard>
    </OnboardingLayout>
  );
}
