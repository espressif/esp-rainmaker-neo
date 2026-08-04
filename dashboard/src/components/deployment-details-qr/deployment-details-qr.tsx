/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useSetState } from "react-use";
import QRCode from "qrcode";
import { Download, QrCode } from "lucide-react";
import {
  Alert,
  Button,
  SectionCard,
  Spinner,
} from "@espressif/dashboard-ui-components/components";
import {
  DEPLOYMENT_QR_DOWNLOAD_FILENAME,
  DEPLOYMENT_QR_OPTIONS,
  DEPLOYMENT_QR_SPINNER_SIZE,
} from "./deployment-details-qr.constants";
import type { DeploymentDetailsQrProps } from "./deployment-details-qr.props";
import { downloadDataUrl } from "./deployment-details-qr.utils";

interface DeploymentDetailsQrState {
  dataUrl: string;
  loading: boolean;
  error: string;
}

const INITIAL_STATE: DeploymentDetailsQrState = {
  dataUrl: "",
  loading: false,
  error: "",
};

/**
 * Pure UI card that renders the given URL as a downloadable QR code. Makes no
 * API calls — the caller supplies the URL.
 */
export default function DeploymentDetailsQr({
  url,
  onReady,
}: DeploymentDetailsQrProps) {
  const { t } = useTranslation("common");
  const [state, setState] =
    useSetState<DeploymentDetailsQrState>(INITIAL_STATE);
  const onReadyRef = useRef(onReady);
  onReadyRef.current = onReady;

  useEffect(() => {
    let isMounted = true;

    const generate = async () => {
      setState({ loading: true, error: "", dataUrl: "" });

      try {
        const dataUrl = await QRCode.toDataURL(url, DEPLOYMENT_QR_OPTIONS);

        if (!isMounted) {
          return;
        }

        setState({ dataUrl, loading: false });
        onReadyRef.current?.(dataUrl);
      } catch {
        if (!isMounted) {
          return;
        }

        setState({
          error: t(
            "deploymentQr.error",
            "Could not generate the QR code. Try again later.",
          ),
          loading: false,
        });
      }
    };

    void generate();

    return () => {
      isMounted = false;
    };
  }, [setState, t, url]);

  const handleDownload = useCallback(() => {
    if (!state.dataUrl) {
      return;
    }

    downloadDataUrl(state.dataUrl, DEPLOYMENT_QR_DOWNLOAD_FILENAME);
  }, [state.dataUrl]);

  const loadingLabel = t("deploymentQr.loadingLabel", "Preparing QR code...");

  if (state.loading) {
    return (
      <div
        className="flex min-h-[300px] items-center justify-center"
        role="status"
        aria-busy="true"
        aria-label={loadingLabel}
      >
        <Spinner size={DEPLOYMENT_QR_SPINNER_SIZE} />
      </div>
    );
  }

  if (state.error) {
    return (
      <Alert type="error" variant="soft" color="error">
        {state.error}
      </Alert>
    );
  }

  if (!state.dataUrl) {
    return null;
  }

  return (
    <SectionCard
      icon={<QrCode className="h-5 w-5" aria-hidden />}
      primaryText={t("deploymentQr.title", "Deployment Details QR Code")}
      actions={
        <Button
          type="button"
          variant="outline"
          color="gray"
          fullWidth={false}
          startIcon={<Download />}
          onClick={handleDownload}
        >
          {t("common:actions.download", "Download")}
        </Button>
      }
      allowCollapse={false}
      size="lg"
      color="mist"
      variant="gradient"
      className="rounded-3xl"
    >
      <div className="flex flex-col gap-8">
        <Alert hideIcon variant="soft" color="gray">
          {t(
            "deploymentQr.infoMessage",
            "Scan or download this QR code to configure a client with the connection details for this deployment.",
          )}
        </Alert>
        <div className="flex justify-center rounded-xl">
          <div className="relative inline-block">
            <span
              aria-hidden="true"
              className="pointer-events-none absolute -left-4 -top-4 h-8 w-8 border-l-6 border-t-6 border-gray rounded-tl-2xl"
            />
            <span
              aria-hidden="true"
              className="pointer-events-none absolute -right-4 -top-4 h-8 w-8 border-r-6 border-t-6 border-gray rounded-tr-2xl"
            />
            <span
              aria-hidden="true"
              className="pointer-events-none absolute -bottom-4 -left-4 h-8 w-8 border-b-6 border-l-6 border-gray rounded-bl-2xl"
            />
            <span
              aria-hidden="true"
              className="pointer-events-none absolute -bottom-4 -right-4 h-8 w-8 border-b-6 border-r-6 border-gray rounded-br-2xl"
            />
            <img
              src={state.dataUrl}
              alt={t("deploymentQr.imageAlt", "Deployment details QR code")}
              className="mx-auto block h-auto max-h-[240px] max-w-full"
            />
          </div>
        </div>
        <div>{/*empty div to add gap below the QR code */}</div>
      </div>
    </SectionCard>
  );
}
