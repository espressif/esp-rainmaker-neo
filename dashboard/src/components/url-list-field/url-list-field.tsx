/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useController, useFormContext } from "react-hook-form";
import { UrlListManager } from "@/components/url-list-manager";
import type { UrlListFieldProps } from "./url-list-field.props";

interface FieldArrayItem {
  value: string;
}

/**
 * react-hook-form adapter for {@link UrlListManager}. Binds the pure manager to
 * a field-array path (`{ value: string }[]`) in the surrounding `<Form>`,
 * mapping to/from a flat `string[]` and surfacing the field's list-level
 * validation error. Use this inside forms; use `UrlListManager` directly
 * anywhere else.
 */
export default function UrlListField({
  name,
  cardTitle,
  cardDescription,
  icon,
  labels,
}: UrlListFieldProps) {
  const { control } = useFormContext();
  const { field, fieldState } = useController({ control, name });

  const items = (field.value as FieldArrayItem[] | undefined) ?? [];
  const value = items.map((item) => item.value);

  const handleChange = (next: string[]) => {
    field.onChange(next.map((url) => ({ value: url })));
  };

  const error = fieldState.error;
  const errorMessage = error?.root?.message ?? error?.message;

  return (
    <UrlListManager
      value={value}
      onChange={handleChange}
      cardTitle={cardTitle}
      cardDescription={cardDescription}
      icon={icon}
      labels={labels}
      error={errorMessage}
    />
  );
}
