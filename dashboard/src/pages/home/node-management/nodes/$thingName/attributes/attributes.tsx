/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { ContentContainer } from "@espressif/dashboard-ui-components/components";
import AttributesSection from "./_components/attributes-section";
import { useRouteParams } from "@/lib/navigation/use-route-params";

export default function ThingAttributesPage() {
  const params = useRouteParams<{ thingName?: string }>();
  const thingName = params.thingName;

  if (!thingName) {
    return null;
  }

  return (
    <ContentContainer maxWidth="md" noGutters>
      <AttributesSection thingName={thingName} />
    </ContentContainer>
  );
}
