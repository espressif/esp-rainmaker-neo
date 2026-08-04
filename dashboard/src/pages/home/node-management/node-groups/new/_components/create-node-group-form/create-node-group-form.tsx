/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { FolderPlus } from "lucide-react";
import {
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  Form,
  ScrollableSections,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import { useCreateNodeGroupForm } from "../../_hooks/use-create-node-group-form";
import { SECTION_BASIC_DETAILS } from "../../_constants/create-node-group-form.constants";
import { CreateNodeGroupStatus } from "../create-node-group-status";
import type { CreateNodeGroupFormProps } from "./create-node-group-form.props";
import { voidFormSubmit } from "@/lib/void-form-submit";

export default function CreateNodeGroupForm(_props: CreateNodeGroupFormProps) {
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
    backToGroups,
    viewGroupDetails,
    editAndRetry,
  } = useCreateNodeGroupForm();

  return (
    <>
      <Dialog open={dialogOpen} onOpenChange={handleDialogOpenChange}>
        <DialogContent
          onInteractOutside={(event) => event.preventDefault()}
          onEscapeKeyDown={(event) => event.preventDefault()}
          className="min-w-2xl"
        >
          <DialogTitle className="sr-only">
            {t("new.status.dialogTitle", "Node group creation status")}
          </DialogTitle>
          <CreateNodeGroupStatus
            status={status}
            result={result}
            errorMessage={errorMessage}
            onBackToGroups={backToGroups}
            onViewGroupDetails={viewGroupDetails}
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
          <ScrollableSections defaultValue={SECTION_BASIC_DETAILS} className="w-full">
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
              startIcon={<FolderPlus />}
              disabled={isSubmitting}
            >
              {isSubmitting
                ? t("new.status.submitting", "Creating…")
                : t("new.submitButton", "Create node group")}
            </Button>
          </div>
        </form>
      </Form>
    </>
  );
}
