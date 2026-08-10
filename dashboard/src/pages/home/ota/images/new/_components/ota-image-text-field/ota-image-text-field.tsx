/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useFormContext } from "react-hook-form";
import { InfoIcon, LockIcon } from "lucide-react";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
  Tooltip,
} from "@espressif/dashboard-ui-components/components";
import type { UploadOtaImageFormValues } from "../../_schema/upload-ota-image-form.schema";
import type { OtaImageTextFieldProps } from "./ota-image-text-field.props";

/** Renders a single text `Input` field wired to the upload form context. */
export function OtaImageTextField({
  name,
  label,
  placeholder,
  required,
  locked,
  tooltip,
}: OtaImageTextFieldProps) {
  const { control } = useFormContext<UploadOtaImageFormValues>();

  const labelContent = tooltip ? (
    <span className="inline-flex items-center gap-1.5">
      {label}
      <Tooltip content={tooltip}>
        <span
          className="inline-flex text-muted-foreground"
          role="img"
          aria-label={tooltip}
          tabIndex={0}
        >
          <InfoIcon className="h-3.5 w-3.5" aria-hidden />
        </span>
      </Tooltip>
    </span>
  ) : (
    label
  );

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
              label={labelContent}
              required={required}
              placeholder={placeholder}
              readOnly={locked}
              endIcon={
                locked ? (
                  <LockIcon className="h-4 w-4 text-muted-foreground" aria-hidden />
                ) : undefined
              }
              error={!!fieldState.error}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
