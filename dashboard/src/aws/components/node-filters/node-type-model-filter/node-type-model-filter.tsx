/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo, useState } from "react";
import type { ComponentProps } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { ArrowRight, Box, ChevronDown, X } from "lucide-react";
import {
  Button,
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@espressif/dashboard-ui-components/components";
import { isolateNestedFormSubmit } from "@/lib/isolate-nested-form-submit";
import type { NodeTypeModelFilterProps } from "./node-type-model-filter.props";

type FormValues = {
  type: string;
  model: string;
};

export function NodeTypeModelFilter({
  value,
  onChange,
}: NodeTypeModelFilterProps) {
  const { t } = useTranslation(["nodes", "common"]);
  const [open, setOpen] = useState(false);

  const schema = useMemo(
    () =>
      z.object({
        type: z
          .string()
          .trim()
          .min(1, t("typeModelTypeRequired", "Type is required")),
        model: z.string(),
      }),
    [t],
  );

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      type: value?.type ?? "",
      model: value?.model ?? "",
    },
  });

  const onSubmit = (data: FormValues) => {
    const modelTrimmed = data.model.trim();
    onChange({
      type: data.type.trim(),
      ...(modelTrimmed ? { model: modelTrimmed } : {}),
    });
    setOpen(false);
  };

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (next) {
      form.reset({
        type: value?.type ?? "",
        model: value?.model ?? "",
      });
    }
  };

  const appliedLabel = (() => {
    if (value == null) {
      return "";
    }
    if (value.model) {
      return `${value.type} / ${value.model}`;
    }
    return value.type;
  })();

  const triggerText = value
    ? appliedLabel
    : t("typeModelFilterTrigger", "Type/Model");

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          color="gray"
          fullWidth={false}
          startIcon={<Box className="h-4 w-4 shrink-0" />}
          endIcon={<ChevronDown className="h-4 w-4 shrink-0" />}
          className="max-w-[min(100%,14rem)]"
          type="button"
          usePrimaryColorOnHover
          size="sm"
        >
          <span
            className="min-w-0 truncate"
            title={value ? appliedLabel : undefined}
          >
            {triggerText}
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80">
        <Form {...(form as unknown as ComponentProps<typeof Form>)}>
          <form
            onSubmit={isolateNestedFormSubmit(form.handleSubmit(onSubmit))}
            className="flex flex-col gap-5"
          >
            <FormField
              control={
                form.control as ComponentProps<
                  typeof FormField<FormValues>
                >["control"]
              }
              name="type"
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Input
                      type="text"
                      label={t("typeModelFieldType", "Type")}
                      placeholder={t("typeModelFieldTypePlaceholder", "Type")}
                      size="sm"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={
                form.control as ComponentProps<
                  typeof FormField<FormValues>
                >["control"]
              }
              name="model"
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Input
                      type="text"
                      label={t("typeModelFieldModel", "Model")}
                      placeholder={t(
                        "typeModelFieldModelPlaceholder",
                        "Model (optional)",
                      )}
                      size="sm"
                      {...field}
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
                startIcon={<X />}
                onClick={() => {
                  form.reset({ type: "", model: "" });
                  onChange(null);
                  setOpen(false);
                }}
              >
                {t("common:actions.clear", "Clear")}
              </Button>
              <Button size="sm" type="submit" endIcon={<ArrowRight />}>
                {t("common:actions.submit", "Submit")}
              </Button>
            </div>
          </form>
        </Form>
      </PopoverContent>
    </Popover>
  );
}
