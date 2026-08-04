/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import { useSetState } from "react-use";
import {
  getCredentialProviderEndpoint,
  getFilesBucket,
  getIotEndpoint,
} from "@/lib/config";
import { useNodeRegistrationHandoffStore } from "@/stores/node-registration-handoff.store";
import { generateBulkMfgZip } from "@/utils/bulk-generate/zip-builder";
import { generateMatterBulkMfgZip } from "@/utils/bulk-generate/matter-zip-builder";

// Filename + MIME type the register page expects for the handed-off CSV.
const NODE_CERTS_CSV_FILENAME = "node_certs.csv";
const NODE_CERTS_CSV_TYPE = "text/csv";
const REGISTER_NODES_ROUTE = "/home/node-management/register/new";

export type GenerateStatus = "idle" | "generating" | "done" | "error";

interface StartFlowParams {
  count: number;
  matter: boolean;
}

interface OrchestrationState {
  status: GenerateStatus;
  dialogOpen: boolean;
  errorMessage: string;
  zipBlob: Blob | null;
  zipName: string;
  nodeCertsCsv: string;
  downloaded: boolean;
}

const initialState: OrchestrationState = {
  status: "idle",
  dialogOpen: false,
  errorMessage: "",
  zipBlob: null,
  zipName: "",
  nodeCertsCsv: "",
  downloaded: false,
};

// The generator emits granular progress, but the UI intentionally shows a fake
// progress bar, so per-phase updates are ignored here.
const ignoreProgress = () => {};

// Pyodide (the NVS packer) is a warm module singleton after the first run, so
// repeat generations finish almost instantly. Hold the fake progress bar for a
// minimum so each run visibly regenerates instead of flashing straight to done.
const MIN_GENERATING_MS = 1200;

const delay = (ms: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, ms));

export function useGenerateOrchestration() {
  const { t } = useTranslation("generate");
  const navigate = useNavigate();
  const setPendingCsvFile = useNodeRegistrationHandoffStore(
    (store) => store.setPendingCsvFile,
  );
  const [state, setState] = useSetState<OrchestrationState>(initialState);

  const startFlow = useCallback(
    async ({ count, matter }: StartFlowParams) => {
      setState({
        status: "generating",
        dialogOpen: true,
        errorMessage: "",
        zipBlob: null,
        zipName: "",
        nodeCertsCsv: "",
        downloaded: false,
      });

      try {
        const mqttHost = getIotEndpoint();
        if (!mqttHost) {
          throw new Error(
            t("errors.noMqttHost", "IoT endpoint not configured"),
          );
        }

        const generate = matter ? generateMatterBulkMfgZip : generateBulkMfgZip;
        const [result] = await Promise.all([
          generate({
            count,
            mqttHost,
            mqttCredHost: getCredentialProviderEndpoint(),
            filesBucket: getFilesBucket(),
            onProgress: ignoreProgress,
          }),
          delay(MIN_GENERATING_MS),
        ]);

        setState({
          status: "done",
          zipBlob: result.blob,
          zipName: result.prefix,
          nodeCertsCsv: result.nodeCertsCsv,
        });
      } catch (error) {
        setState({
          status: "error",
          errorMessage:
            error instanceof Error
              ? error.message
              : t("status.errorFallback", "Generation failed."),
        });
      }
    },
    [setState, t],
  );

  const download = useCallback(() => {
    if (!state.zipBlob) {
      return;
    }
    const url = URL.createObjectURL(state.zipBlob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `${state.zipName}.zip`;
    document.body.appendChild(anchor);
    anchor.click();
    document.body.removeChild(anchor);
    URL.revokeObjectURL(url);
    setState({ downloaded: true });
  }, [state.zipBlob, state.zipName, setState]);

  // Hand the generated node_certs CSV to the register page (pre-uploaded) and
  // navigate there. Gated in the UI behind a prior download so users don't lose
  // the un-recoverable credentials package.
  const registerNodes = useCallback(() => {
    if (!state.nodeCertsCsv) {
      return;
    }
    const csvFile = new File([state.nodeCertsCsv], NODE_CERTS_CSV_FILENAME, {
      type: NODE_CERTS_CSV_TYPE,
    });
    setPendingCsvFile(csvFile);
    setState({ ...initialState });
    void navigate({ to: REGISTER_NODES_ROUTE });
  }, [state.nodeCertsCsv, setPendingCsvFile, setState, navigate]);

  const reset = useCallback(() => {
    setState({ ...initialState });
  }, [setState]);

  const handleDialogOpenChange = useCallback(
    (open: boolean) => {
      if (open) {
        return;
      }
      reset();
    },
    [reset],
  );

  return {
    status: state.status,
    dialogOpen: state.dialogOpen,
    errorMessage: state.errorMessage,
    downloaded: state.downloaded,
    startFlow,
    download,
    registerNodes,
    reset,
    handleDialogOpenChange,
  };
}
