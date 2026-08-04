/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useFormContext } from "react-hook-form";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
} from "@espressif/dashboard-ui-components/components";
import type { UploadOtaImageFormValues } from "../../_schema/upload-ota-image-form.schema";
import type { OtaImageTextFieldProps } from "./ota-image-text-field.props";

/** Renders a single text `Input` field wired to the upload form context. */
export function OtaImageTextField({
  name,
  label,
  placeholder,
  required,
}: OtaImageTextFieldProps) {
  const { control } = useFormContext<UploadOtaImageFormValues>();

  return (
    <FormField
      control={control}
      name={name}
      render={({ field, fieldState }) => (
        <FormItem>
          <FormControl>
            <Input
              {...field}
              value={field.value ?? ""}
              label={label}
              required={required}
              placeholder={placeholder}
              error={!!fieldState.error}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
