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
import { Check, Pencil, X } from "lucide-react";
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
import type { EditThingTagPopoverProps } from "./edit-thing-tag-popover.props";

type FormValues = {
  value: string;
};

export default function EditThingTagPopover({
  thingName,
  type,
  tagKey,
  initialValue,
}: EditThingTagPopoverProps) {
  const { t } = useTranslation(["nodes", "common"]);
  const [open, setOpen] = useState(false);
  const mutation = useUpdateNodeTags(thingName);

  const schema = useMemo(
    () =>
      z.object({
        value: z
          .string()
          .trim()
          .min(1, t("tags.form.valueRequired", "Value is required"))
          .refine(
            (candidate) => candidate !== initialValue,
            t("tags.form.noChange", "Enter a new value to save"),
          ),
      }),
    [t, initialValue],
  );

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { value: initialValue },
  });

  const closePopover = useCallback(() => {
    setOpen(false);
    form.reset({ value: initialValue });
    mutation.reset();
  }, [form, initialValue, mutation]);

  const handleOpenChange = useCallback(
    (next: boolean) => {
      if (next) {
        setOpen(true);
        form.reset({ value: initialValue });
        mutation.reset();
        return;
      }
      closePopover();
    },
    [closePopover, form, initialValue, mutation],
  );

  const onSubmit = useCallback(
    (values: FormValues) => {
      mutation.mutate(
        { [type]: { [tagKey]: values.value.trim() } },
        { onSuccess: closePopover },
      );
    },
    [mutation, type, tagKey, closePopover],
  );

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          color="gray"
          fullWidth={false}
          aria-label={t("tags.editTag", "Edit tag")}
          tooltip={t("tags.editTag", "Edit tag")}
        >
          <Pencil className="h-4 w-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80">
        <Form {...(form as unknown as ComponentProps<typeof Form>)}>
          <form
            onSubmit={isolateNestedFormSubmit(form.handleSubmit(onSubmit))}
            className="flex flex-col gap-4"
          >
            <Input
              type="text"
              label={t("tags.form.keyLabel", "Key")}
              value={tagKey}
              size="sm"
              readOnly
              disabled
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
                startIcon={<Check className="h-4 w-4" />}
                loading={mutation.isPending}
              >
                {t("tags.form.save", "Save")}
              </Button>
            </div>
          </form>
        </Form>
      </PopoverContent>
    </Popover>
  );
}
