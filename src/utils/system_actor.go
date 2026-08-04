// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

import "github.com/espressif/esp-cloud-common/go/rbac/rbac"

const SYSTEM_ACTOR = "system"

type SystemActor struct {
	SystemID    string
	Permissions rbac.EntityPermissions
}

func NewSystemActor() *SystemActor {
	s := &SystemActor{SystemID: SYSTEM_ACTOR}
	s.Permissions.SetAllow("*", "*")
	return s
}

func (sa *SystemActor) GetID() string {
	return sa.SystemID
}

func (sa *SystemActor) GetPermissions() *rbac.EntityPermissions {
	return &sa.Permissions
}
