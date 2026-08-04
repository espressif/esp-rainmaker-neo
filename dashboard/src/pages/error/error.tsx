/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { motion } from "framer-motion";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import { AnimatedCard } from "@espressif/dashboard-ui-components/components";
import { Button } from "@espressif/dashboard-ui-components/components";
import { MoveRight } from "lucide-react";
import { logout } from "@/lib/auth";

export default function ErrorPage() {
  const { t } = useTranslation("common");
  const navigate = useNavigate();

  const handleGoHome = () => {
    void navigate({ to: "/home" });
  };

  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center space-y-4">
      <motion.div
        initial={{ opacity: 0, scale: 0.9 }}
        animate={{ opacity: 1, scale: 1 }}
        transition={{ duration: 0.3 }}
        className="text-center"
      >
        <div className="flex flex-col items-center justify-center gap-4 max-w-md">
          <AnimatedCard type="error" iconSize={96} />
          <h2 className="text-2xl font-medium">{t("errorTitle", "Something went wrong")}</h2>
          <p className="text-muted-foreground">{t("errorMessage", "An unexpected error occurred. Please try again later.")}</p>
          <p className="text-muted-foreground">{t("logoutRecommendation", "If the problem persists, it could be due to a temporary issue with your session. Please logout and try again.")}</p>
          <Button
            endIcon={<MoveRight className="w-4 h-4" />}
            onClick={handleGoHome}
          >
            {t("goHome", "Go Home")}
          </Button>
          <Button
            variant="outline"
            endIcon={<MoveRight className="w-4 h-4" />}
            onClick={() => logout()}
          >
            {t("logout", "Logout")}
          </Button>
        </div>
      </motion.div>
    </div>
  );
}
