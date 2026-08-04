/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useFormContext } from "react-hook-form";
import {
  Alert,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
  SelectableCardList,
  type SelectableCardListItem,
} from "@espressif/dashboard-ui-components/components";
import { CustomIcon } from "@/components/custom-icon";
import {
  MATTER_DEFAULT_PRODUCT_ID,
  MATTER_DEFAULT_VENDOR_ID,
} from "@/utils/matter-gen";
import { hex4 } from "../../../../_utils/matter-id-format";
import type { GenerateNodesFormValues } from "../../../../_schema/generate-nodes-form.schema";

const MATTER_CARD_ID = "matter";

export function MatterOptionsField() {
  const { t } = useTranslation("generate");
  const { control, watch } = useFormContext<GenerateNodesFormValues>();
  const matter = watch("matter");

  const cards = useMemo<SelectableCardListItem[]>(
    () => [
      {
        id: MATTER_CARD_ID,
        icon: <CustomIcon type="matter" size={20} />,
        primaryText: t(
          "fields.matter",
          "Generate Matter-enabled device data",
        ),
        secondaryText: t(
          "fields.matterHint",
          "Produce Matter attestation certificates and commissioning data alongside RainMaker credentials.",
        ),
      },
    ],
    [t],
  );

  return (
    <div className="flex flex-col gap-4">
      <FormField
        control={control}
        name="matter"
        render={({ field }) => (
          <FormItem>
            <FormControl>
              <SelectableCardList
                allowMultiple
                data={cards}
                value={field.value ? [MATTER_CARD_ID] : []}
                onChange={(next) =>
                  field.onChange(next.includes(MATTER_CARD_ID))
                }
                aria-label={t(
                  "fields.matterAriaLabel",
                  "Matter device data",
                )}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      {matter && (
        <>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Input
              readOnly
              label={t("fields.vendorId", "DAC Vendor ID (VID)")}
              value={hex4(MATTER_DEFAULT_VENDOR_ID)}
            />
            <Input
              readOnly
              label={t("fields.productId", "DAC Product ID (PID)")}
              value={hex4(MATTER_DEFAULT_PRODUCT_ID)}
            />
          </div>
          <Alert
            type="info"
            variant="soft"
            title={t(
              "matterNotice.title",
              "Matter: evaluation-only attestation certificates",
            )}
            description={t(
              "matterNotice.description",
              "Matter device attestation certificates (DACs) are signed with the CHIP Test PAI, which commissioners accept only for evaluation and testing. Production Matter devices require valid DACs issued under a real attestation chain — contact Espressif to obtain production Matter credentials.",
            )}
          />
        </>
      )}
    </div>
  );
}
