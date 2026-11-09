// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	// Blank imports register each capability module's node-register hook from init().
	_ "github.com/espressif/esp-rainmaker-neo/src/bridge/hooks"
	_ "github.com/espressif/esp-rainmaker-neo/src/controller/hooks"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/nodeadmin/bulk_container"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
)

func main() {
	config, err := bulk_container.NewContainerConfigFromEnv()
	if err != nil {
		utils.Rlog.Error().Err(err).Msg("Failed to create container config from environment variables")
		return
	}

	err = bulk_container.HandleContainer(config)
	if err != nil {
		utils.Rlog.Error().Err(err).Msg("Failed to handle container")
	}
}
