/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { ArrowLeftIcon, ArrowRightIcon, PlusIcon } from "lucide-react";
import {
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  Form,
  ScrollableSections,
  SectionCard,
  Separator,
  StatusCardList,
  ButtonGroup,
  type StatusCardListItem,
} from "@espressif/dashboard-ui-components/components";
import { useRegisterNodesForm } from "../../_hooks/use-register-nodes-form";
import { RegistrationResultAlert } from "../registration-result-alert";
import type { RegisterNodesFormProps } from "./register-nodes-form.props";
import { StepErrorActions } from "./step-error-actions";
import { voidFormSubmit } from "@/lib/void-form-submit";

export default function RegisterNodesForm({
  initialCertificateFile,
}: RegisterNodesFormProps) {
  const {
    t,
    form,
    sections,
    processState,
    showFooter,
    isSubmitting,
    handleSubmit,
    handleDialogOpenChange,
    retryFrom,
    closeDialog,
    goToRegistrationJobs,
    registerMore,
  } = useRegisterNodesForm(initialCertificateFile);

  const retryLabel = t("common:actions.retry", "Retry");
  const closeLabel = t("common:actions.close", "Close");

  const displayedSteps: StatusCardListItem[] = useMemo(
    () =>
      processState.steps.map((step) => {
        if (step.state !== "error") {return step;}
        return {
          ...step,
          action: (
            <StepErrorActions
              onRetry={() => retryFrom(step.id as Parameters<typeof retryFrom>[0])}
              onClose={closeDialog}
              retryLabel={retryLabel}
              closeLabel={closeLabel}
            />
          ),
        };
      }),
    [processState.steps, retryFrom, closeDialog, retryLabel, closeLabel],
  );

  return (
    <>
      <Dialog
        open={processState.dialogOpen}
        onOpenChange={handleDialogOpenChange}
      >
        <DialogContent
          onEscapeKeyDown={(e) => e.preventDefault()}
          onInteractOutside={(e) => e.preventDefault()}
          className="lg:min-w-2xl xl:min-w-2xl sm:max-w-lg overflow-hidden"
        >
          <DialogTitle className="sr-only">
            {t(
              "new.progress.dialogTitle",
              "Registration progress",
            )}
          </DialogTitle>

          <StatusCardList data={displayedSteps} />

          {processState.resultAlert && (
            <>
              <Separator />
              <RegistrationResultAlert result={processState.resultAlert} />
            </>
          )}

          {showFooter && (
            <ButtonGroup className="w-full">
              <Button
                type="button"
                variant="outline"
                size="lg"
                startIcon={<ArrowLeftIcon />}
                onClick={goToRegistrationJobs}
                color="gray"
              >
                {t(
                  "new.progress.backToJobs",
                  "Back to Registration jobs",
                )}
              </Button>
              <Button
                type="button"
                size="lg"
                startIcon={<PlusIcon />}
                onClick={registerMore}
                color="secondary"
              >
                {t("new.progress.registerMore", "Register more nodes")}
              </Button>
            </ButtonGroup>
          )}
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

            {sections.map(({ id, Icon, label, Content: SectionContent }) => (
              <ScrollableSections.Content key={id} id={id}>
                <SectionCard
                  allowCollapse={false}
                  icon={<Icon className="h-6 w-6" />}
                  primaryText={label}
                  color="silver"
                  variant="outline"
                >
                  <SectionContent />
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
              disabled={isSubmitting}
            >
              {isSubmitting
                ? t("new.form.submitting", "Registering…")
                : t("new.form.submit", "Register")}
            </Button>
          </div>
        </form>
      </Form>
    </>
  );
}
