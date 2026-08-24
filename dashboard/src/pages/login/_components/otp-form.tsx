/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowRightIcon } from "lucide-react";
import {
  Button,
  Checkbox,
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
  Label,
} from "@espressif/dashboard-ui-components/components";
import {
  getAuthSchemaMessages,
  getOtpRequestSchema,
  type OtpRequestSchema,
} from "@/api";
import { voidFormSubmit } from "@/lib/void-form-submit";

interface OtpFormProps {
  destination: string;
  allowKeepMeSignedIn: boolean;
  keepSignedIn: boolean;
  onKeepSignedInChange: (checked: boolean) => void;
  isSubmitting: boolean;
  isResending: boolean;
  onSubmit: (code: string) => void;
  onResend: () => void;
  onUsePassword: () => void;
}

/** Step 3: exchange the emailed code for tokens. */
export default function OtpForm({
  destination,
  allowKeepMeSignedIn,
  keepSignedIn,
  onKeepSignedInChange,
  isSubmitting,
  isResending,
  onSubmit,
  onResend,
  onUsePassword,
}: OtpFormProps) {
  const { t } = useTranslation(["login", "common"]);
  const schema = useMemo(() => getOtpRequestSchema(getAuthSchemaMessages(t)), [t]);
  const form = useForm<OtpRequestSchema>({
    resolver: zodResolver(schema),
    defaultValues: { code: "" },
  });

  return (
    <Form {...form}>
      <form
        onSubmit={voidFormSubmit(form.handleSubmit((data) => onSubmit(data.code)))}
        className="space-y-6"
      >
        {destination && (
          <p className="text-sm text-muted-foreground">
            {t("otpSentTo", { defaultValue: "We sent a code to {{destination}}.", destination })}
          </p>
        )}

        <FormField
          control={form.control}
          name="code"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <Input
                  type="text"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  label={t("otpLabel", "One-time code")}
                  placeholder={t("otpPlaceholder", "Enter the 6-digit code")}
                  {...field}
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

        <div className="flex flex-col gap-2 text-sm">
          <Button type="button" variant="link" loading={isResending} onClick={onResend}>
            {t("resendCode", "Resend code")}
          </Button>
          {/* Offered unconditionally rather than gated on Cognito reporting a PASSWORD
              factor: an admin who has no password gets the same generic "incorrect
              username or password" a wrong one earns, so the button costs nothing and
              keeps this screen identical for every address typed into it. */}
          <Button type="button" variant="link" onClick={onUsePassword}>
            {t("usePasswordInstead", "Sign in with password instead")}
          </Button>
        </div>
      </form>
    </Form>
  );
}
