/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'

export default function Goodbye() {
  const { t } = useTranslation("common")

  return (
    <motion.h1
      initial={{ opacity: 0, y: -20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5 }}
      className="text-4xl font-bold text-primary"
    >
      {t('goodbye', "Good Bye")}
    </motion.h1>
  )
}

