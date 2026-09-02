/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { MailCheck } from "lucide-react";
import OnboardingLayout from "@/containers/onboarding/onboarding-layout";
import { OnboardingCard } from "@/components/onboarding-card";
import { BackToSignInLink } from "@/components/back-to-signin-link";
import { appConfig } from "@/lib/app-config";
import { otpErrorMessage } from "@/lib/auth";
import { useResendCooldown } from "@/hooks/use-resend-cooldown";
import { useSigninFlowStore } from "@/stores/signin-flow.store";
import { resolveErrorMessage } from "../_utils/signin-errors";
import { useSigninFlow } from "../_hooks/use-signin-flow";
import {
  signinEntryRoute,
  useSigninStepGuard,
} from "../_hooks/use-signin-step-guard";
import { SigninAlerts } from "../_components/signin-alerts";
import { OtpForm } from "./_components/otp-form";

/**
 * Screen 2 — OTP entry. Reached only after a send succeeded (the OTP-send
 * rule), which is also why the resend cooldown starts armed: arrival means a
 * code was just mailed.
 */
export default function LoginOtp() {
  const { t } = useTranslation(["login", "common"]);
  const flow = useSigninFlow();
  const username = useSigninFlowStore((s) => s.username);
  const destination = useSigninFlowStore((s) => s.destination);
  const challenges = useSigninFlowStore((s) => s.challenges);
  const keepSignedIn = useSigninFlowStore((s) => s.keepSignedIn);
  const setKeepSignedIn = useSigninFlowStore((s) => s.setKeepSignedIn);
  const flowMessage = useSigninFlowStore((s) => s.flowMessage);
  const cooldown = useResendCooldown(true);

  const guardSatisfied = useSigninStepGuard("session");
  if (!guardSatisfied) {
    return null;
  }

  const allowKeepMeSignedIn = appConfig.customAuth?.allowKeepMeSignedIn ?? false;
  // The typed address is the recognisable one; Cognito's masked `destination`
  // (`a***@e***.com`) covers entry points where it was never typed.
  const sentTo = username || destination || "";

  const errorMessage = resolveErrorMessage(
    t,
    null,
    otpErrorMessage(flow.error) ?? flowMessage,
    flow.error,
  );

  return (
    <OnboardingLayout>
      <OnboardingCard
        icon={<MailCheck className="w-6 h-6" />}
        title={t("otp.title", "Check your email")}
        description={t("otp.sentTo", {
          defaultValue: "Enter the 8 digit code sent to {{email}}",
          email: sentTo,
        })}
        actions={<BackToSignInLink to={signinEntryRoute()} />}
      >
        <SigninAlerts errorMessage={errorMessage} />

        <OtpForm
          allowKeepMeSignedIn={allowKeepMeSignedIn}
          keepSignedIn={keepSignedIn}
          onKeepSignedInChange={setKeepSignedIn}
          isSubmitting={flow.isVerifying}
          isResending={flow.isSending}
          resendSecondsLeft={cooldown.secondsLeft}
          canUsePassword={challenges.includes("PASSWORD")}
          onSubmit={flow.verify}
          onResend={() => flow.resend(cooldown.restart)}
          onUsePassword={flow.choosePassword}
        />
      </OnboardingCard>
    </OnboardingLayout>
  );
}
