/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useGetAlexaConfig } from "@/api/integrations";
import { AlexaMainContent } from "./_components/alexa-main-content";
import { AlexaConfigFormSheet } from "./_components/alexa-config-form-sheet";

export default function Alexa() {
  const { data, isLoading, error } = useGetAlexaConfig();
  const [isFormOpen, setIsFormOpen] = useState(false);

  return (
    <div className="py-6">
      <AlexaMainContent
        data={data}
        isLoading={isLoading}
        error={error}
        onConfigure={() => setIsFormOpen(true)}
      />

      {isFormOpen && (
        <AlexaConfigFormSheet
          initialData={data}
          onClose={() => setIsFormOpen(false)}
        />
      )}
    </div>
  );
}
