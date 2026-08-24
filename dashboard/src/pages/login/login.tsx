/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { LogIn } from "lucide-react";
import OnboardingLayout from "@/containers/onboarding/onboarding-layout";
import { Alert, Button } from "@espressif/dashboard-ui-components/components";
import { OnboardingCard } from "@/components/onboarding-card";
import { appConfig } from "@/lib/app-config";
import {
  useSignin,
  resetLogoutFlag,
  type SigninRequestSchema,
  type SigninResponse,
} from "@/api";
import {
  storeAuthTokens,
  storeKeepSignedIn,
  getKeepSignedIn,
  consumeRedirectPath,
  otpErrorMessage,
  type LocalizedMessage,
} from "@/lib/auth";
import { useUserStore } from "@/stores/user.store";
import { parseLoginSearch, type LoginSearch } from "./_schema/login-search.schema";
import { useSigninFlow } from "./_hooks/use-signin-flow";
import IdentifyForm from "./_components/identify-form";
import OtpForm from "./_components/otp-form";
import PasswordForm from "./_components/password-form";

/**
 * The single error message this page shows, in priority order: a rejected
 * password submit, then a failure from the identify/choose/otp flow, then a
 * generic fallback for anything else `useSignin` raised. `null` means nothing
 * failed, so no alert should render.
 */
function resolveErrorMessage(
  t: TFunction,
  loginError: string | null,
  flowError: LocalizedMessage | null,
  signinError: Error | null,
): string | null {
  if (!loginError && !flowError && !signinError) {
    return null;
  }
  if (loginError) {
    return loginError;
  }
  if (flowError) {
    return t(flowError.key, flowError.fallback);
  }
  return (
    signinError?.message ||
    t(
      "common:errorMessage",
      "An unexpected error occurred. Please try again later.",
    )
  );
}

/**
 * The success banner for the two ways account settings can send an admin back here.
 * "success" is a password change; "set" is a first password adopted by an admin who
 * previously had none — reusing the "reset" wording for that case would be false, so
 * each outcome gets its own title and message.
 */
function passwordOutcomeCopy(
  t: TFunction,
  outcome: LoginSearch["reset"],
): { title: string; message: string } | null {
  if (outcome === "set") {
    return {
      title: t("setSuccessTitle", "Password set"),
      message: t(
        "setSuccessMessage",
        "Your password has been set. Sign in with it, or with a code, from now on.",
      ),
    };
  }
  if (outcome === "success") {
    return {
      title: t("resetSuccessTitle", "Password updated"),
      message: t(
        "resetSuccessMessage",
        "Your password has been updated. Sign in with your new password.",
      ),
    };
  }
  return null;
}

interface SigninStepPanelProps {
  flow: ReturnType<typeof useSigninFlow>;
  allowKeepMeSignedIn: boolean;
  keepSignedIn: boolean;
  onKeepSignedInChange: (checked: boolean) => void;
  isPasswordSubmitting: boolean;
  onPasswordSubmit: (data: SigninRequestSchema) => void;
}

/**
 * Renders whichever step the state machine is on, plus the "start over" link
 * once the admin has moved past the first screen. Split out of `Login` purely
 * to keep that component's branch count readable.
 */
function SigninStepPanel({
  flow,
  allowKeepMeSignedIn,
  keepSignedIn,
  onKeepSignedInChange,
  isPasswordSubmitting,
  onPasswordSubmit,
}: SigninStepPanelProps) {
  const { t } = useTranslation("login");

  return (
    <>
      {flow.state.step === "identify" && (
        <IdentifyForm
          defaultUsername={flow.state.username}
          isSubmitting={flow.isStarting}
          onSubmit={flow.identify}
        />
      )}
      {flow.state.step === "otp" && (
        <OtpForm
          destination={flow.state.destination ?? ""}
          allowKeepMeSignedIn={allowKeepMeSignedIn}
          keepSignedIn={keepSignedIn}
          onKeepSignedInChange={onKeepSignedInChange}
          isSubmitting={flow.isVerifying}
          isResending={flow.isSending}
          onSubmit={flow.verify}
          onResend={flow.resend}
          onUsePassword={flow.choosePassword}
        />
      )}
      {flow.state.step === "password" && (
        <PasswordForm
          defaultUsername={flow.state.username}
          allowKeepMeSignedIn={allowKeepMeSignedIn}
          keepSignedIn={keepSignedIn}
          onKeepSignedInChange={onKeepSignedInChange}
          isSubmitting={isPasswordSubmitting}
          onSubmit={onPasswordSubmit}
        />
      )}

      {flow.state.step !== "identify" && (
        <Button type="button" variant="link" onClick={flow.restart}>
          {t("useAnotherAddress", "Use a different email")}
        </Button>
      )}
    </>
  );
}

export default function Login() {
  const { t } = useTranslation(["login", "common"]);
  const navigate = useNavigate();
  const location = useLocation();
  const allowKeepMeSignedIn = appConfig.customAuth?.allowKeepMeSignedIn ?? false;
  // Prefilled from the last sign-in on this browser, so the choice does not have to be
  // made again every time. Only ever written on a successful submit.
  const [keepSignedIn, setKeepSignedIn] = useState(getKeepSignedIn);
  const [loginError, setLoginError] = useState<string | null>(null);
  const loginSearch = useMemo(
    () => parseLoginSearch(location.search),
    [location.search],
  );
  const passwordOutcomeMessage = passwordOutcomeCopy(t, loginSearch.reset);

  const signinMutation = useSignin();
  // `signinMutation` is a fresh object every render (react-query's `useMutation`
  // return value), but `.reset` is bound once and kept stable — pulling it out lets
  // the effect below depend on the stable function instead of the unstable object,
  // which would otherwise re-run (and therefore reset the mutation) on every render.
  const { reset: resetSigninMutation } = signinMutation;
  const setLoggedInUserName = useUserStore((s) => s.setLoggedInUserName);

  const handleAuthenticated = useCallback(
    (username: string, response: SigninResponse) => {
      // The refresh token is persisted only on explicit opt-in: it is the one
      // credential that outlives the browser session. `handleAuthenticated` is the
      // single completion point for both the password and the OTP path, so
      // `keepSignedIn` governs whichever one just succeeded.
      storeAuthTokens({
        accessToken: response.access_token,
        idToken: response.id_token,
        refreshToken: keepSignedIn ? response.refresh_token : undefined,
      });
      storeKeepSignedIn(keepSignedIn);
      setLoggedInUserName(username);
      resetLogoutFlag();

      // `?redirect=` wins: it is the destination the dead session handed over,
      // and it survives the hard navigation that `logout()` performs.
      const redirectPath = loginSearch.redirect ?? consumeRedirectPath();
      void navigate({ to: redirectPath || "/home" });
    },
    [keepSignedIn, loginSearch.redirect, navigate, setLoggedInUserName],
  );

  const flow = useSigninFlow({ onAuthenticated: handleAuthenticated });

  // `loginError` and `signinMutation`'s own error are both local to the password
  // path, so leaving the password screen by any route the reducer offers (currently
  // just `restart`) has to drop both explicitly. Otherwise a stale "invalid
  // credentials" alert — or, worse, `signinMutation.error`'s raw untranslated
  // Cognito message — would bleed onto whichever step comes next.
  useEffect(() => {
    if (flow.state.step !== "password") {
      setLoginError(null);
      resetSigninMutation();
    }
  }, [flow.state.step, resetSigninMutation]);

  const onPasswordSubmit = (data: SigninRequestSchema) => {
    setLoginError(null);
    signinMutation.mutate(data, {
      onSuccess: (response) => {
        // Backend returns 200 with only token_type for invalid credentials
        if (!response.access_token || !response.id_token) {
          signinMutation.reset();
          setLoginError(
            t("invalidCredentials", "Invalid username or password"),
          );
          return;
        }

        handleAuthenticated(data.username, response);
      },
    });
  };

  const flowError = otpErrorMessage(flow.error);
  const errorMessage = resolveErrorMessage(
    t,
    loginError,
    flowError,
    signinMutation.error,
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
      >
        {errorMessage && (
          <Alert
            title={t("errorTitle", "Unable to login")}
            type="error"
            description={errorMessage}
            hideIcon
            className="border-none shadow-none mb-4"
          />
        )}

        {passwordOutcomeMessage && !errorMessage && (
          <Alert
            title={passwordOutcomeMessage.title}
            type="success"
            description={passwordOutcomeMessage.message}
            hideIcon
            className="border-none shadow-none mb-4"
          />
        )}

        <SigninStepPanel
          flow={flow}
          allowKeepMeSignedIn={allowKeepMeSignedIn}
          keepSignedIn={keepSignedIn}
          onKeepSignedInChange={setKeepSignedIn}
          isPasswordSubmitting={signinMutation.isPending}
          onPasswordSubmit={onPasswordSubmit}
        />
      </OnboardingCard>
    </OnboardingLayout>
  );
}
