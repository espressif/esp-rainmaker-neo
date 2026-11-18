/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { toast } from "@espressif/dashboard-ui-components/components";
import { useConnectAlexaLwa, useDisconnectAlexaLwa } from "@/api/integrations";
import { LwaConnectError, requestLwaAuthorizationCode } from "@/lib/oauth";
import { normalizeApiError } from "@/lib/normalize-api-error";
import {
  buildAlexaLwaConnectionSchema,
  type AlexaLwaConnectionValues,
} from "./alexa-lwa-connection.schema";

function consentErrorMessage(error: unknown, t: TFunction): string {
  if (!(error instanceof LwaConnectError)) {
    return t("alexa.lwa.errors.popupFailed", "Could not open the Amazon sign-in window.");
  }
  switch (error.reason) {
    case "popupBlocked":
      return t("alexa.lwa.errors.popupBlocked", "The browser blocked the Amazon sign-in window. Allow pop-ups for this site and try again.");
    case "denied":
      return t("alexa.lwa.errors.denied", "Amazon did not grant access: {{detail}}", { detail: error.detail ?? "-" });
    case "timeout":
      return t("alexa.lwa.errors.timeout", "No answer from Amazon within five minutes. Connect again.");
    default:
      return t("alexa.lwa.errors.noCode", "Amazon returned without an authorization code. Connect again.");
  }
}

export function useAlexaLwaConnection() {
  const { t } = useTranslation("voice-assistants");

  const schema = useMemo(() => buildAlexaLwaConnectionSchema(t), [t]);
  const form = useForm<AlexaLwaConnectionValues>({
    resolver: zodResolver(schema),
    defaultValues: { lwa_client_id: "", lwa_client_secret: "" },
  });

  const connectMutation = useConnectAlexaLwa();
  const disconnectMutation = useDisconnectAlexaLwa();
  const [consentError, setConsentError] = useState<string>();

  const runConnect = async (values: AlexaLwaConnectionValues) => {
    setConsentError(undefined);
    connectMutation.reset();
    let authorization: { code: string; redirectUri: string };
    try {
      authorization = await requestLwaAuthorizationCode(values.lwa_client_id);
    } catch (error) {
      // A newer click owns the state now.
      if (error instanceof LwaConnectError && error.reason === "superseded") {
        return;
      }
      setConsentError(consentErrorMessage(error, t));
      return;
    }
    connectMutation.mutate(
      {
        ...values,
        lwa_authorization_code: authorization.code,
        lwa_redirect_uri: authorization.redirectUri,
      },
      {
        onSuccess: () => {
          toast.success(t("alexa.lwa.connectSuccess", "Amazon account connected."));
          form.reset();
        },
      },
    );
  };

  const connect = () => void form.handleSubmit(runConnect)();

  // Lets `mutateAsync` throw so ConfirmationDialog stays open on failure.
  const disconnect = async () => {
    await disconnectMutation.mutateAsync();
    toast.success(t("alexa.lwa.disconnectSuccess", "Amazon account disconnected."));
  };

  const connectErrorMessage =
    consentError ??
    (connectMutation.isError
      ? normalizeApiError(
          connectMutation.error,
          t("alexa.lwa.connectError", "Failed to connect the Amazon account."),
        )
      : undefined);

  const disconnectErrorMessage = disconnectMutation.isError
    ? normalizeApiError(
        disconnectMutation.error,
        t("alexa.lwa.disconnectError", "Failed to disconnect the Amazon account."),
      )
    : null;

  return {
    form,
    connect,
    // Not while the popup is open: its close is undetectable, and a second click supersedes the first request.
    isConnecting: connectMutation.isPending,
    connectErrorMessage,
    disconnect,
    isDisconnecting: disconnectMutation.isPending,
    disconnectErrorMessage,
  };
}
