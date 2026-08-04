/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Cpu, Sparkles } from "lucide-react";
import {
  Alert,
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  Form,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import { useGenerateNodesForm } from "../../_hooks/use-generate-nodes-form";
import { EachDeviceSummary } from "../each-device-summary";
import { GenerateStatus } from "../generate-status";
import { DeviceCountField } from "./_components/device-count-field";
import { MatterOptionsField } from "./_components/matter-options-field";
import type { GenerateNodesFormProps } from "./generate-nodes-form.props";
import { voidFormSubmit } from "@/lib/void-form-submit";

export default function GenerateNodesForm(_props: GenerateNodesFormProps) {
  const {
    t,
    form,
    status,
    dialogOpen,
    errorMessage,
    downloaded,
    isGenerating,
    isIotEndpointConfigured,
    handleSubmit,
    handleDialogOpenChange,
    download,
    registerNodes,
    retry,
  } = useGenerateNodesForm();

  return (
    <>
      <Dialog open={dialogOpen} onOpenChange={handleDialogOpenChange}>
        <DialogContent
          onInteractOutside={(event) => event.preventDefault()}
          onEscapeKeyDown={(event) => event.preventDefault()}
          className="min-w-2xl"
        >
          <DialogTitle className="sr-only">
            {t("status.dialogTitle", "Node generation status")}
          </DialogTitle>
          <GenerateStatus
            status={status}
            errorMessage={errorMessage}
            downloaded={downloaded}
            onDownload={download}
            onRegisterNodes={registerNodes}
            onRetry={retry}
          />
        </DialogContent>
      </Dialog>

      <Form {...form}>
        <form
          noValidate
          onSubmit={voidFormSubmit(form.handleSubmit(handleSubmit))}
          className="flex w-full flex-col gap-6 lg:flex-row lg:items-start lg:gap-8"
        >
          <div className="flex w-full min-w-0 flex-col gap-6 lg:w-[55%]">
            <SectionCard
              allowCollapse={false}
              icon={<Cpu className="h-6 w-6" />}
              primaryText={t("form.title", "Batch configuration")}
              secondaryText={t(
                "form.description",
                "Choose how many test nodes to generate and what device data to include.",
              )}
              color="silver"
              variant="outline"
            >
              <div className="flex flex-col gap-6">
                <DeviceCountField />
                <MatterOptionsField />
              </div>
            </SectionCard>

            {!isIotEndpointConfigured && (
              <Alert
                type="warning"
                variant="soft"
                title={t(
                  "config.missingEndpointTitle",
                  "IoT endpoint not configured",
                )}
                description={t(
                  "config.missingEndpointDescription",
                  "Test node generation needs this deployment's IoT endpoint, which isn't configured. Set it in the environment or runtime configuration and reload before generating.",
                )}
              />
            )}

            <div className="flex justify-end border-t border-border pt-6">
              <Button
                type="submit"
                size="lg"
                fullWidth={false}
                startIcon={<Sparkles className="h-4 w-4" />}
                disabled={isGenerating || !isIotEndpointConfigured}
              >
                {t("submit", "Generate")}
              </Button>
            </div>
          </div>

          <div className="w-full min-w-0 lg:sticky lg:top-24 lg:w-[45%]">
            <EachDeviceSummary />
          </div>
        </form>
      </Form>
    </>
  );
}
