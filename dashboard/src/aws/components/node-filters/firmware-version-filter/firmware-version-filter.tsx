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
import { ArrowRight, ChevronDown, Cpu, X } from "lucide-react";
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
import type { FirmwareVersionFilterProps } from "./firmware-version-filter.props";

type FormValues = {
  firmwareVersion: string;
};

export function FirmwareVersionFilter({
  value,
  onChange,
  disabled = false,
}: FirmwareVersionFilterProps) {
  const { t } = useTranslation(["nodes", "common"]);
  const [open, setOpen] = useState(false);

  const schema = useMemo(
    () =>
      z.object({
        firmwareVersion: z
          .string()
          .trim()
          .min(1, t("firmwareVersionRequired", "Firmware version is required")),
      }),
    [t],
  );

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      firmwareVersion: value ?? "",
    },
  });

  const onSubmit = (data: FormValues) => {
    onChange(data.firmwareVersion.trim());
    setOpen(false);
  };

  const handleOpenChange = (next: boolean) => {
    if (disabled) {
      setOpen(false);
      return;
    }
    setOpen(next);
    if (next) {
      form.reset({
        firmwareVersion: value ?? "",
      });
    }
  };

  const triggerText = value
    ? value
    : t("firmwareVersionFilterTrigger", "Firmware version");

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          color="gray"
          usePrimaryColorOnHover
          fullWidth={false}
          startIcon={<Cpu className="h-4 w-4 shrink-0" />}
          endIcon={<ChevronDown className="h-4 w-4 shrink-0" />}
          className="max-w-[min(100%,14rem)]"
          type="button"
          size="sm"
          disabled={disabled}
        >
          <span className="min-w-0 truncate" title={value ?? undefined}>
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
              name="firmwareVersion"
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Input
                      type="text"
                      label={t("firmwareVersionFieldLabel", "Firmware version")}
                      placeholder={t(
                        "firmwareVersionFieldPlaceholder",
                        "Enter firmware version",
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
                  form.reset({ firmwareVersion: "" });
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
