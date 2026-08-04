/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useFormContext } from "react-hook-form";
import { CircleCheck, PackageCheck } from "lucide-react";
import { SectionCard } from "@espressif/dashboard-ui-components/components";
import type { GenerateNodesFormValues } from "../../_schema/generate-nodes-form.schema";

export function EachDeviceSummary() {
  const { t } = useTranslation("generate");
  const { watch } = useFormContext<GenerateNodesFormValues>();
  const matter = watch("matter");

  const items = useMemo(
    () =>
      matter
        ? [
            t(
              "info.matterDac",
              "DAC + PAI device attestation certificates (signed by the CHIP test PAI)",
            ),
            t(
              "info.matterCommissioning",
              "Matter commissioning data (discriminator, passcode, SPAKE2+ verifier)",
            ),
            t(
              "info.matterNvs",
              "Merged Matter + RainMaker NVS factory partition binary",
            ),
            t(
              "info.matterQr",
              "Matter onboarding QR code and manual pairing code",
            ),
          ]
        : [
            t(
              "info.cert",
              "ECDSA P-256 key pair and certificate (signed by auto-generated CA)",
            ),
            t("info.nvs", "NVS partition binary (12 KB) with credentials"),
            t("info.qr", "QR code for BLE provisioning"),
          ],
    [matter, t],
  );

  return (
    <SectionCard
      allowCollapse={false}
      icon={<PackageCheck className="h-6 w-6" />}
      primaryText={t("info.title", "Each device will get:")}
      secondaryText={t(
        "info.description",
        "Everything needed to flash and provision one test node.",
      )}
      color="info"
      variant="gradient"
      className="border !border-info/20"
    >
      <ul className="flex flex-col gap-3">
        {items.map((item) => (
          <li
            key={item}
            className="flex items-start gap-2 text-sm text-muted-foreground"
          >
            <CircleCheck
              className="mt-0.5 h-4 w-4 shrink-0 text-success"
              aria-hidden
            />
            <span>{item}</span>
          </li>
        ))}
      </ul>
    </SectionCard>
  );
}
