// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/gomega"
)

type TestFormation struct {
	Users map[string]TFUser `json:"users"`
}

type TFUser struct {
	Groups map[string]TFGroup `json:"grps"`
}

type TFGroup struct {
	Nodes           []string              `json:"nodes"`
	SubGroups       map[string]TFSubGroup `json:"sub_grps"`
	SharedPrimary   []string              `json:"shared_primary"`
	SharedSecondary []string              `json:"shared_secondary"`
}

type TFSubGroup struct {
	Nodes  []string `json:"nodes"`
	Shared []string `json:"shared"`
}

type TFOutput struct {
	UserCtx   map[string]*rmngctx.RmngContext
	Groups    map[string]*group.Group
	SubGroups map[string]*group.SubGroup
}

func (tf *TestFormation) Setup() TFOutput {
	tfOutput := TFOutput{
		UserCtx:   make(map[string]*rmngctx.RmngContext),
		Groups:    make(map[string]*group.Group),
		SubGroups: make(map[string]*group.SubGroup),
	}

	for uid, tfUser := range tf.Users {
		u := user.NewUser(uid)
		tfOutput.UserCtx[uid] = rmngctx.NewRmngContext(u)
		tfUser.Setup(tfOutput.UserCtx[uid], &tfOutput)
	}

	return tfOutput
}

func (u *TFUser) Setup(ctx *rmngctx.RmngContext, tfOutput *TFOutput) {
	var err error
	for grpName, tfGroup := range u.Groups {
		tfOutput.Groups[grpName], err = group.CreateGroupForUser(ctx, grpName)
		Expect(err).To(BeNil())
		tfGroup.Setup(ctx, tfOutput.Groups[grpName], tfOutput)

	}
}

func (g *TFGroup) Setup(context *rmngctx.RmngContext, grp *group.Group, tfOutput *TFOutput) {
	for _, node := range g.Nodes {
		context.SetAllow(utils.NodeAll, node)
		_, err := group.AddNode(context, grp.GroupID, node, nil)
		Expect(err).To(BeNil())
	}

	for sgrp_name, subGroup := range g.SubGroups {
		sgrp, err := group.CreateSubGroup(context, grp.GroupID, sgrp_name)
		Expect(err).To(BeNil())
		tfOutput.SubGroups[sgrp_name] = sgrp
		subGroup.Setup(context, grp, sgrp)
	}

	for _, targetUserID := range g.SharedPrimary {
		_, err := group.ShareGroup(context, grp.GroupID, targetUserID, utils.GroupPrimaryAccess, auth.UserInfo{})
		Expect(err).To(BeNil())

		targetUserCtx := rmngctx.NewRmngContext(user.NewUser(targetUserID))
		sharingRequests, err := group.GetMySharingRequests(targetUserCtx)
		Expect(err).To(BeNil())
		Expect(sharingRequests).To(HaveLen(1))

		err = group.ApproveSharingRequest(targetUserCtx, sharingRequests[0].SharingRequestID)
		Expect(err).To(BeNil())
	}

	for _, targetUserID := range g.SharedSecondary {
		_, err := group.ShareGroup(context, grp.GroupID, targetUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
		Expect(err).To(BeNil())

		targetUserCtx := rmngctx.NewRmngContext(user.NewUser(targetUserID))
		sharingRequests, err := group.GetMySharingRequests(targetUserCtx)
		Expect(err).To(BeNil())
		Expect(sharingRequests).To(HaveLen(1))

		err = group.ApproveSharingRequest(targetUserCtx, sharingRequests[0].SharingRequestID)
		Expect(err).To(BeNil())
	}
}

func (s *TFSubGroup) Setup(context *rmngctx.RmngContext, grp *group.Group, sgrp *group.SubGroup) {
	for _, node := range s.Nodes {
		_, err := group.UpdateNodeAndSubgroup(context, grp.GroupID, node, sgrp.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
		Expect(err).To(BeNil())
	}

	for _, targetUserID := range s.Shared {
		_, err := group.ShareSubGroup(context, grp.GroupID, sgrp.SubGroupID, targetUserID, auth.UserInfo{})
		Expect(err).To(BeNil())

		targetUserCtx := rmngctx.NewRmngContext(user.NewUser(targetUserID))
		sharingRequests, err := group.GetMySharingRequests(targetUserCtx)
		Expect(err).To(BeNil())
		Expect(sharingRequests).To(HaveLen(1))

		err = group.ApproveSharingRequest(targetUserCtx, sharingRequests[0].SharingRequestID)
		Expect(err).To(BeNil())
	}
}
