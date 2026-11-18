/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { SimpleCard } from "@espressif/dashboard-ui-components/components";
import { LWA_CALLBACK_CHANNEL, readLwaCallback } from "@/lib/oauth";

/**
 * Where Amazon returns the "Connect Amazon account" popup. It only relays the authorization
 * response to the dashboard tab that opened the popup, which checks the state and redeems
 * the code, then closes itself.
 */
export default function AlexaLwaCallback() {
  const { t } = useTranslation("alexa-lwa-callback");

  useEffect(() => {
    const channel = new BroadcastChannel(LWA_CALLBACK_CHANNEL);
    channel.postMessage(readLwaCallback(new URLSearchParams(window.location.search)));
    channel.close();
    window.close();
  }, []);

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <SimpleCard
        className="w-full max-w-lg"
        title={t("title", "Amazon account")}
        description={t(
          "description",
          "The Amazon sign-in result has been passed back to the dashboard.",
        )}
      >
        <p className="text-sm text-muted-foreground">
          {t("closeHint", "You can close this window and return to the dashboard.")}
        </p>
      </SimpleCard>
    </div>
  );
}
