/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useFormContext } from "react-hook-form";
import { useTranslation } from "react-i18next";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  InputPassword,
  RequirementList,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import type { ChangePasswordRequestSchema } from "@/api";
import type { ChangePasswordFieldsProps } from "./change-password-fields.props";

/**
 * The three password inputs. Reads the form from context so the field markup stays
 * independent of how the parent wires up `useForm`.
 *
 * The new-password field shows the requirements checklist in place of `FormMessage`:
 * the checklist already lists every rule and marks the failing ones, so rendering both
 * would repeat the same sentence twice. The other two fields keep `FormMessage`.
 */
export default function ChangePasswordFields({
  mode,
  requirementItems,
}: ChangePasswordFieldsProps) {
  const { t } = useTranslation("account-settings");
  const { control } = useFormContext<ChangePasswordRequestSchema>();

  return (
    <div className="flex flex-col gap-5">
      {mode === "change" && (
        <FormField
          control={control}
          name="old_password"
          render={({ field, fieldState }) => (
            <FormItem>
              <FormControl>
                <InputPassword
                  {...field}
                  autoComplete="current-password"
                  required
                  label={t("password.currentPasswordLabel", "Current password")}
                  placeholder={t(
                    "password.currentPasswordPlaceholder",
                    "Enter your current password",
                  )}
                  error={!!fieldState.error}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      <FormField
        control={control}
        name="new_password"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <InputPassword
                {...field}
                autoComplete="new-password"
                required
                label={t("password.newPasswordLabel", "New password")}
                placeholder={t(
                  "password.newPasswordPlaceholder",
                  "Enter a new password",
                )}
                error={!!fieldState.error}
              />
            </FormControl>
            <SectionCard
              className="mt-4"
              primaryText={t(
                "password.requirementsLabel",
                "Password requirements",
              )}
              allowCollapse={false}
              color="silver"
              variant="soft"
              size="sm"
            >
              <RequirementList
                items={requirementItems}
                metLabel={t("password.requirementMet", "Requirement met")}
                unmetLabel={t(
                  "password.requirementUnmet",
                  "Requirement not met",
                )}
              />
            </SectionCard>
          </FormItem>
        )}
      />

      <FormField
        control={control}
        name="confirm_password"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <InputPassword
                {...field}
                autoComplete="new-password"
                required
                label={t("password.confirmPasswordLabel", "Confirm new password")}
                placeholder={t(
                  "password.confirmPasswordPlaceholder",
                  "Re-enter the new password",
                )}
                error={!!fieldState.error}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  );
}
