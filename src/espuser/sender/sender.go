// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package sender resolves which SES identity the IdP sends mail from. SES owns which identities
// exist and whether each is verified; espuser-admin-config records which one is active per email
// category. Spec: src_esp_user/misc/specs/email-sender.md.
package sender

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/sesutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/admin_config_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

type Sender = sesutil.Identity

const CategoryGlobal = admin_config_db.CategoryGlobal

type ActiveSenders struct {
	Senders map[string]string `json:"senders"`
}

type Service struct {
	config *admin_config_db.AdminConfigDB
}

func NewService(rmngCtx *rmngctx.RmngContext) *Service {
	return &Service{config: admin_config_db.NewAdminConfigDB(rmngCtx)}
}

// GetActiveSenders returns the active sender per category. For the global category with no
// explicit selection it falls back to the sole verified sender; senders is a hint (pass the
// already-fetched list to avoid a second SES call), and is listed from SES only if nil.
func (s *Service) GetActiveSenders(ctx context.Context, senders []Sender) (ActiveSenders, error) {
	rows, err := s.config.GetAll(admin_config_db.ConfigEmailSender)
	if err != nil {
		return ActiveSenders{}, err
	}
	active := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.Value != "" {
			active[r.Subtype] = r.Value
		}
	}
	if _, ok := active[admin_config_db.CategoryGlobal]; !ok {
		if senders == nil {
			if senders, err = sesutil.ListIdentities(ctx); err != nil {
				return ActiveSenders{}, err
			}
		}
		if verified := verifiedEmails(senders); len(verified) == 1 {
			active[admin_config_db.CategoryGlobal] = verified[0]
		}
	}
	return ActiveSenders{Senders: active}, nil
}

func verifiedEmails(senders []Sender) []string {
	var out []string
	for _, s := range senders {
		if s.IsVerified() {
			out = append(out, s.Email)
		}
	}
	return out
}
