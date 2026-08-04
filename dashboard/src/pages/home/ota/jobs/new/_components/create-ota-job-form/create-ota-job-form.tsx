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
import { useCreateOtaJobForm } from "../../_hooks/use-create-ota-job-form";
import { CreateOtaJobStatus } from "../create-ota-job-status";
import type { CreateOtaJobFormProps } from "./create-ota-job-form.props";
import { voidFormSubmit } from "@/lib/void-form-submit";

export default function CreateOtaJobForm({
  firmwareKey,
}: CreateOtaJobFormProps) {
  const {
    t,
    form,
    sections,
    status,
    dialogOpen,
    errorMessage,
    result,
    isSubmitting,
    handleSubmit,
    handleDialogOpenChange,
    backToJobs,
    viewJobDetails,
    editAndRetry,
  } = useCreateOtaJobForm(firmwareKey);

  return (
    <>
      <Dialog open={dialogOpen} onOpenChange={handleDialogOpenChange}>
        <DialogContent
          onInteractOutside={(event) => event.preventDefault()}
          onEscapeKeyDown={(event) => event.preventDefault()}
          className="min-w-2xl"
        >
          <DialogTitle className="sr-only">
            {t("createOtaJobPage.status.dialogTitle", "OTA job creation status")}
          </DialogTitle>
          <CreateOtaJobStatus
            status={status}
            result={result}
            errorMessage={errorMessage}
            onBackToJobs={backToJobs}
            onViewJobDetails={viewJobDetails}
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
          <ScrollableSections defaultValue="basic-details" className="w-full">
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

            {sections.map(
              ({ id, Icon, label, secondaryText, Content: SectionContent }) => (
                <ScrollableSections.Content key={id} id={id}>
                  <SectionCard
                    allowCollapse={false}
                    icon={<Icon className="h-6 w-6" />}
                    primaryText={label}
                    secondaryText={secondaryText}
                    color="silver"
                    variant="outline"
                  >
                    <SectionContent />
                  </SectionCard>
                </ScrollableSections.Content>
              ),
            )}
          </ScrollableSections>

          <div className="flex justify-end border-t border-border pt-6">
            <Button
              type="submit"
              size="lg"
              fullWidth={false}
              animateEndIconOnHover
              endIcon={<ArrowRightIcon />}
              disabled={isSubmitting}
            >
              {isSubmitting
                ? t("createOtaJobPage.status.submitting", "Creating…")
                : t("createOtaJobPage.submitButton", "Create OTA Job")}
            </Button>
          </div>
        </form>
      </Form>
    </>
  );
}
