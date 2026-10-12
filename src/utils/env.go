// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

import "os"

func SetTestEnvVars(envVars map[string]string) {
	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
}

func ResetTestEnvVars(envVars map[string]string) {
	for key := range envVars {
		if err := os.Unsetenv(key); err != nil {
			panic(err)
		}
	}
}
