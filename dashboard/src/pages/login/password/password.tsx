/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Trans, useTranslation } from "react-i18next";
import { KeyRound } from "lucide-react";
import OnboardingLayout from "@/containers/onboarding/onboarding-layout";
import { OnboardingCard } from "@/components/onboarding-card";
import { BackToSignInLink } from "@/components/back-to-signin-link";
import { appConfig } from "@/lib/app-config";
import { useForgotPassword, useSignin } from "@/api";
import { useSigninFlowStore } from "@/stores/signin-flow.store";
import { resolveErrorMessage } from "../_utils/signin-errors";
import { useCompleteSignin } from "../_hooks/use-complete-signin";
import {
  signinEntryRoute,
  useSigninStepGuard,
} from "../_hooks/use-signin-step-guard";
import { SigninAlerts } from "../_components/signin-alerts";
import { PasswordForm } from "./_components/password-form";

/**
 * Screen 4 — password entry. The address is settled by now (account chip, not a
 * field); only the password is asked for. Error state and mutations are local
 * to this page, so leaving it drops them by construction.
 */
export default function LoginPassword() {
  const { t } = useTranslation(["login", "common"]);
  const navigate = useNavigate();
  const completeSignin = useCompleteSignin();
  const signinMutation = useSignin();
  const forgotMutation = useForgotPassword();
  const [loginError, setLoginError] = useState<string | null>(null);
  const username = useSigninFlowStore((s) => s.username);
  const keepSignedIn = useSigninFlowStore((s) => s.keepSignedIn);
  const setKeepSignedIn = useSigninFlowStore((s) => s.setKeepSignedIn);
  const flowMessage = useSigninFlowStore((s) => s.flowMessage);

  const guardSatisfied = useSigninStepGuard("username");
  if (!guardSatisfied) {
    return null;
  }

  const allowKeepMeSignedIn =
    appConfig.customAuth?.allowKeepMeSignedIn ?? false;

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
        actions={<BackToSignInLink to={signinEntryRoute()} />}
      >
        <SigninAlerts errorMessage={errorMessage} />

        <div className="space-y-6">
          <p className="text-sm text-muted-foreground">
            <Trans
              t={t}
              i18nKey="password.description"
              values={{ email: username }}
              components={{
                strong: (
                  <span className="font-semibold text-foreground break-words" />
                ),
              }}
            />
          </p>

          <PasswordForm
            allowKeepMeSignedIn={allowKeepMeSignedIn}
            keepSignedIn={keepSignedIn}
            onKeepSignedInChange={setKeepSignedIn}
            isSubmitting={signinMutation.isPending}
            isRequestingReset={forgotMutation.isPending}
            onSubmit={onPasswordSubmit}
            onForgotPassword={onForgotPassword}
          />
        </div>
      </OnboardingCard>
    </OnboardingLayout>
  );
}
