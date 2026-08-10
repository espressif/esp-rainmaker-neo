/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { ArrowRightIcon } from "lucide-react";
import {
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  Form,
  ScrollableSections,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import { useUploadOtaImageForm } from "../../_hooks/use-upload-ota-image-form";
import { UploadOtaImageStatus } from "../upload-ota-image-status";
import type { UploadOtaImageFormProps } from "./upload-ota-image-form.props";
import { voidFormSubmit } from "@/lib/void-form-submit";

export default function UploadOtaImageForm(_props: UploadOtaImageFormProps) {
  const {
    t,
    form,
    sections,
    lockedFields,
    status,
    dialogOpen,
    errorMessage,
    result,
    isUploading,
    handleSubmit,
    handleDialogOpenChange,
    backToImages,
    createOtaWithImage,
    editAndRetry,
  } = useUploadOtaImageForm();

  return (
    <>
      <Dialog open={dialogOpen} onOpenChange={handleDialogOpenChange}>
        <DialogContent
          onInteractOutside={(event) => event.preventDefault()}
          onEscapeKeyDown={(event) => event.preventDefault()}
          className="min-w-2xl"
        >
          <DialogTitle className="sr-only">
            {t("upload.dialogTitle", "OTA image upload status")}
          </DialogTitle>
          <UploadOtaImageStatus
            status={status}
            result={result}
            errorMessage={errorMessage}
            onBackToImages={backToImages}
            onCreateOta={createOtaWithImage}
            onEditAndRetry={editAndRetry}
          />
        </DialogContent>
      </Dialog>

      <Form {...form}>
        <form
          noValidate
          onSubmit={voidFormSubmit(form.handleSubmit(handleSubmit))}
          className="mx-auto flex w-full flex-col gap-8"
        >
          <ScrollableSections defaultValue="select-image" className="w-full">
            <ScrollableSections.Tabs>
              {sections.map(({ id, Icon, label }) => (
                <ScrollableSections.Tab key={id} id={id} label={label}>
                  <span className="flex items-center gap-2">
                    <Icon className="h-4 w-4 shrink-0" aria-hidden />
                    <span>{label}</span>
                  </span>
                </ScrollableSections.Tab>
              ))}
            </ScrollableSections.Tabs>

            {sections.map(({ id, Icon, label, Content: SectionContent }) => (
              <ScrollableSections.Content key={id} id={id}>
                <SectionCard
                  allowCollapse={false}
                  icon={<Icon className="h-6 w-6" />}
                  primaryText={label}
                  color="silver"
                  variant="outline"
                >
                  <SectionContent lockedFields={lockedFields} />
                </SectionCard>
              </ScrollableSections.Content>
            ))}
          </ScrollableSections>

          <div className="flex justify-end border-t border-border pt-6">
            <Button
              type="submit"
              size="lg"
              fullWidth={false}
              animateEndIconOnHover
              endIcon={<ArrowRightIcon />}
              disabled={isUploading}
            >
              {isUploading
                ? t("form.submitting", "Uploading…")
                : t("form.submit", "Upload OTA Image")}
            </Button>
          </div>
        </form>
      </Form>
    </>
  );
}
