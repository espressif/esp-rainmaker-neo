/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowRightIcon, KeyRoundIcon } from "lucide-react";
import {
  Button,
  Checkbox,
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  InputOTP,
  Label,
} from "@espressif/dashboard-ui-components/components";
import {
  getAuthSchemaMessages,
  getOtpRequestSchema,
  type OtpRequestSchema,
} from "@/api";
import { ResendCodeHint } from "@/components/resend-code-hint";
import { voidFormSubmit } from "@/lib/void-form-submit";
import type { OtpFormProps } from "./otp-form.props";

const OTP_LENGTH = 8;

/** Screen 2's form: exchange the emailed code for tokens. */
export default function OtpForm({
  allowKeepMeSignedIn,
  keepSignedIn,
  onKeepSignedInChange,
  isSubmitting,
  isResending,
  resendSecondsLeft,
  canUsePassword,
  onSubmit,
  onResend,
  onUsePassword,
}: OtpFormProps) {
  const { t } = useTranslation(["login", "common"]);
  const schema = useMemo(
    () => getOtpRequestSchema(getAuthSchemaMessages(t)),
    [t],
  );
  const form = useForm<OtpRequestSchema>({
    resolver: zodResolver(schema),
    defaultValues: { code: "" },
  });

  const resendControl = (
    <ResendCodeHint
      isCoolingDown={resendSecondsLeft > 0}
      countdownLabel={t("otp.resendIn", {
        defaultValue: "Resend code in {{seconds}}s",
        seconds: resendSecondsLeft,
      })}
      resendLabel={t("resendCode", "Resend code")}
      isResending={isResending}
      onResend={onResend}
    />
  );

  return (
    <Form {...form}>
      <form
        onSubmit={voidFormSubmit(
          form.handleSubmit((data) => onSubmit(data.code)),
        )}
        className="space-y-6"
      >
        <FormField
          control={form.control}
          name="code"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <InputOTP
                  length={OTP_LENGTH}
                  label={t("otpLabel", "Verification code")}
                  autoFocus
                  autoComplete="one-time-code"
                  value={field.value}
                  onChange={field.onChange}
                  hintContent={resendControl}
                  expand
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {allowKeepMeSignedIn && (
          <div className="flex items-center space-x-2">
            <Checkbox
              id="keep-signed-in"
              checked={keepSignedIn}
              onCheckedChange={(checked) =>
                onKeepSignedInChange(checked === true)
              }
            />
            <Label
              htmlFor="keep-signed-in"
              className="text-sm font-normal cursor-pointer"
            >
              {t("keepSignedInLabel", "Keep me signed in")}
            </Label>
          </div>
        )}

        <div className="flex flex-col gap-3">
          <Button
            type="submit"
            loading={isSubmitting}
            size="lg"
            endIcon={<ArrowRightIcon className="w-4 h-4" />}
            animateEndIconOnHover={true}
            loadingIndicator="progress-bar"
          >
            {t("otpSubmit", "Verify")}
          </Button>

          {canUsePassword && (
            <Button
              type="button"
              variant="outline"
              size="lg"
              startIcon={<KeyRoundIcon className="w-4 h-4" />}
              onClick={onUsePassword}
            >
              {t("usePasswordInstead", "Sign in with password")}
            </Button>
          )}
        </div>
      </form>
    </Form>
  );
}
