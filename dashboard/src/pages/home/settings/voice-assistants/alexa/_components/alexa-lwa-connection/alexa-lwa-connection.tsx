/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { ExternalLink, Unlink } from "lucide-react";
import {
  Alert,
  Button,
  ConfirmationDialog,
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
  InputPassword,
  Link,
  MonospaceContent,
} from "@espressif/dashboard-ui-components/components";
import { CustomIcon } from "@/components/custom-icon";
import { getLwaRedirectUri } from "@/lib/oauth";
import { LWA_SECURITY_PROFILE_DOCS_URL } from "../../alexa.constants";
import { useAlexaLwaConnection } from "./use-alexa-lwa-connection";
import type { AlexaLwaConnectionProps } from "./alexa-lwa-connection.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

/**
 * The Amazon developer account automatic mode signs in as. Connecting asks Amazon for consent
 * once in a popup and the backend keeps the resulting token, so later runs need nothing here.
 * Rendered inside the create-skill form, so it holds no `<form>` of its own.
 */
export default function AlexaLwaConnection({ connectedClientId }: AlexaLwaConnectionProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);
  const {
    form,
    connect,
    isConnecting,
    connectErrorMessage,
    disconnect,
    isDisconnecting,
    disconnectErrorMessage,
  } = useAlexaLwaConnection();

  if (connectedClientId) {
    return (
      <div className="flex flex-col gap-3">
        <MonospaceContent
          title={t("alexa.lwa.connectedTitle", "Connected Amazon account (security profile)")}
          value={connectedClientId}
          color="mist"
        />
        <ConfirmationDialog
          title={t("alexa.lwa.disconnectTitle", "Disconnect the Amazon account?")}
          description={t(
            "alexa.lwa.disconnectDescription",
            "The deployment forgets its Amazon sign-in, so automatic setup stops working until you connect again. The skill and its configuration stay. To revoke the access at Amazon as well, remove it under your Amazon account's Login with Amazon settings.",
          )}
          confirmButtonText={t("alexa.lwa.disconnectButton", "Disconnect")}
          cancelButtonText={t("common:actions.cancel", "Cancel")}
          confirmButtonColor="error"
          onConfirm={disconnect}
          onCancel={() => {}}
          isLoading={isDisconnecting}
          error={disconnectErrorMessage}
        >
          <Button
            type="button"
            variant="outline"
            color="error"
            size="sm"
            fullWidth={false}
            className="self-start"
            startIcon={<Unlink className={ICON_CLASS} aria-hidden />}
          >
            {t("alexa.lwa.disconnectButton", "Disconnect")}
          </Button>
        </ConfirmationDialog>
      </div>
    );
  }

  return (
    <Form {...form}>
      <div className="flex flex-col gap-5">
        <FormField
          control={form.control}
          name="lwa_client_id"
          render={({ field, fieldState }) => (
            <FormItem>
              <FormControl>
                <Input
                  {...field}
                  label={t("alexa.autoCreate.clientId", "Security Profile Client ID")}
                  required
                  autoComplete="off"
                  placeholder="amzn1.application-oa2-client...."
                  hintContent={
                    <span className="inline-flex items-center gap-1 whitespace-nowrap">
                      {t("alexa.autoCreate.clientIdHint", "From your security profile.")}
                      <Link
                        to={LWA_SECURITY_PROFILE_DOCS_URL}
                        variant="ghost"
                        underline
                        className="text-primary"
                        endIcon={<ExternalLink className="h-3 w-3" aria-hidden />}
                      >
                        {t("alexa.lwa.docsLink", "How to create one")}
                      </Link>
                    </span>
                  }
                  error={!!fieldState.error}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="lwa_client_secret"
          render={({ field, fieldState }) => (
            <FormItem>
              <FormControl>
                <InputPassword
                  {...field}
                  label={t("alexa.autoCreate.clientSecret", "Security Profile Client Secret")}
                  required
                  autoComplete="new-password"
                  placeholder={t(
                    "alexa.autoCreate.clientSecretPlaceholder",
                    "Enter the Security Profile client secret",
                  )}
                  error={!!fieldState.error}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <MonospaceContent
          title={t("alexa.lwa.returnUrl", "Allowed Return URL (add to your security profile)")}
          value={getLwaRedirectUri()}
          color="silver"
        />

        {connectErrorMessage ? (
          <Alert type="error" variant="soft" color="error">
            {connectErrorMessage}
          </Alert>
        ) : null}

        <Button
          type="button"
          variant="outline"
          fullWidth={false}
          className="self-start"
          onClick={connect}
          loading={isConnecting}
          startIcon={<CustomIcon type="amazon" size={16} aria-hidden />}
        >
          {t("alexa.lwa.connectButton", "Connect Amazon account")}
        </Button>
      </div>
    </Form>
  );
}
