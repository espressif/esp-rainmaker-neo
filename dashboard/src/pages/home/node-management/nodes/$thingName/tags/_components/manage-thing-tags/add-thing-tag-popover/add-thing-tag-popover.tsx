/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo, useState } from "react";
import type { ComponentProps } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { ArrowRight, Plus, X } from "lucide-react";
import {
  Alert,
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
import { useUpdateNodeTags } from "@/api/node-tags";
import { isolateNestedFormSubmit } from "@/lib/isolate-nested-form-submit";
import type { AddThingTagPopoverProps } from "./add-thing-tag-popover.props";

type FormValues = {
  key: string;
  value: string;
};

const EMPTY_VALUES: FormValues = { key: "", value: "" };

export default function AddThingTagPopover({
  thingName,
  type,
  existingKeys,
}: AddThingTagPopoverProps) {
  const { t } = useTranslation(["nodes", "common"]);
  const [open, setOpen] = useState(false);
  const mutation = useUpdateNodeTags(thingName);

  const existingKeysSet = useMemo(() => new Set(existingKeys), [existingKeys]);

  const schema = useMemo(
    () =>
      z.object({
        key: z
          .string()
          .trim()
          .min(1, t("tags.form.keyRequired", "Key is required"))
          .regex(
            /^[^:\s]+$/,
            t(
              "tags.form.keyFormat",
              "Key cannot contain spaces or colons",
            ),
          )
          .refine(
            (candidate) => !existingKeysSet.has(candidate),
            t(
              "tags.form.keyExists",
              "A tag with this key already exists",
            ),
          ),
        value: z
          .string()
          .trim()
          .min(1, t("tags.form.valueRequired", "Value is required")),
      }),
    [t, existingKeysSet],
  );

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: EMPTY_VALUES,
  });

  const closePopover = useCallback(() => {
    setOpen(false);
    form.reset(EMPTY_VALUES);
    mutation.reset();
  }, [form, mutation]);

  const handleOpenChange = useCallback(
    (next: boolean) => {
      if (next) {
        setOpen(true);
        form.reset(EMPTY_VALUES);
        mutation.reset();
        return;
      }
      closePopover();
    },
    [closePopover, form, mutation],
  );

  const onSubmit = useCallback(
    (values: FormValues) => {
      mutation.mutate(
        { [type]: { [values.key.trim()]: values.value.trim() } },
        { onSuccess: closePopover },
      );
    },
    [mutation, type, closePopover],
  );

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          fullWidth={false}
          startIcon={<Plus className="h-4 w-4 shrink-0" />}
        >
          {t("tags.addTag", "Add tag")}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80">
        <Form {...(form as unknown as ComponentProps<typeof Form>)}>
          <form
            onSubmit={isolateNestedFormSubmit(form.handleSubmit(onSubmit))}
            className="flex flex-col gap-4"
          >
            <FormField
              control={
                form.control as ComponentProps<
                  typeof FormField<FormValues>
                >["control"]
              }
              name="key"
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Input
                      type="text"
                      label={t("tags.form.keyLabel", "Key")}
                      placeholder={t(
                        "tags.form.keyPlaceholder",
                        "e.g. environment",
                      )}
                      size="sm"
                      required
                      autoFocus
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
              name="value"
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Input
                      type="text"
                      label={t("common:columns.value", "Value")}
                      placeholder={t(
                        "tags.form.valuePlaceholder",
                        "e.g. production",
                      )}
                      size="sm"
                      required
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            {mutation.error ? (
              <Alert
                type="error"
                variant="soft"
                color="error"
                description={
                  mutation.error.message ||
                  t("tags.form.saveError", "Failed to save tag")
                }
              />
            ) : null}
            <div className="flex justify-end gap-2">
              <Button
                type="button"
                size="sm"
                variant="outline"
                color="gray"
                fullWidth={false}
                startIcon={<X className="h-4 w-4" />}
                onClick={closePopover}
                disabled={mutation.isPending}
              >
                {t("common:actions.cancel", "Cancel")}
              </Button>
              <Button
                type="submit"
                size="sm"
                fullWidth={false}
                endIcon={<ArrowRight className="h-4 w-4" />}
                loading={mutation.isPending}
              >
                {t("common:actions.add", "Add")}
              </Button>
            </div>
          </form>
        </Form>
      </PopoverContent>
    </Popover>
  );
}
