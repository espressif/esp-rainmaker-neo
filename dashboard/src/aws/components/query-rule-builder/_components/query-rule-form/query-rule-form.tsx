/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { ArrowRight, X } from "lucide-react";
import {
  Button,
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@espressif/dashboard-ui-components/components";
import { isolateNestedFormSubmit } from "@/lib/isolate-nested-form-submit";
import { advancedSearchFieldsData } from "@/aws/components/advanced-indices-search/things-indices-search-config";
import { QueryRuleValueField } from "../query-rule-value-field";
import type { QueryRuleFormProps } from "./query-rule-form.props";

type RuleFormValues = {
  field: string;
  value: string;
};

const EMPTY_RULE: RuleFormValues = { field: "", value: "" };

export default function QueryRuleForm({ onSubmit, onClear }: QueryRuleFormProps) {
  const { t } = useTranslation("common");

  const schema = useMemo(
    () =>
      z.object({
        field: z
          .string()
          .min(1, t("queryRuleBuilder.errors.typeRequired", "Type is required.")),
        value: z
          .string()
          .trim()
          .min(1, t("queryRuleBuilder.errors.valueRequired", "Value is required.")),
      }),
    [t],
  );

  const form = useForm<RuleFormValues>({
    resolver: zodResolver(schema),
    defaultValues: EMPTY_RULE,
  });

  const fieldName = form.watch("field");

  const handleSubmit = (values: RuleFormValues) => {
    onSubmit({ field: values.field, value: values.value.trim() });
    form.reset(EMPTY_RULE);
  };

  const handleClear = () => {
    form.reset(EMPTY_RULE);
    onClear?.();
  };

  return (
    <Form {...form}>
      <form
        onSubmit={isolateNestedFormSubmit(form.handleSubmit(handleSubmit))}
        className="flex flex-col gap-5"
      >
        <FormField
          control={form.control}
          name="field"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("queryRuleBuilder.typeLabel", "Type")}</FormLabel>
              <Select
                value={field.value || undefined}
                onValueChange={(next) => {
                  field.onChange(next);
                  form.setValue("value", "");
                }}
              >
                <FormControl>
                  <SelectTrigger size="sm" ref={field.ref}>
                    <SelectValue
                      placeholder={t(
                        "queryRuleBuilder.typePlaceholder",
                        "Select a type",
                      )}
                    />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  {advancedSearchFieldsData.map((definition) => (
                    <SelectItem key={definition.name} value={definition.name}>
                      {t(definition.labelKey, definition.label)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="value"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("common:columns.value", "Value")}</FormLabel>
              <FormControl>
                <QueryRuleValueField
                  fieldName={fieldName}
                  value={field.value}
                  onChange={field.onChange}
                  disabled={!fieldName}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <div className="flex gap-3">
          <Button
            size="sm"
            type="button"
            variant="outline"
            color="error"
            startIcon={<X className="h-4 w-4 shrink-0" aria-hidden />}
            onClick={handleClear}
          >
            {t("common:actions.clear", "Clear")}
          </Button>
          <Button
            size="sm"
            type="submit"
            endIcon={<ArrowRight className="h-4 w-4 shrink-0" aria-hidden />}
          >
            {t("common:actions.submit", "Submit")}
          </Button>
        </div>
      </form>
    </Form>
  );
}
