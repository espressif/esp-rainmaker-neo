// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package user

import (
	"context"
	"errors"
	"fmt"
	"github.com/espressif/esp-cloud-common/go/rbac/rbac"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/validation"

	"github.com/aws/aws-lambda-go/events"
)

// TODO: Clean up this file/package. Giving cyclic dependency.
// Found the cycle: espuser/auth/admin.go imports rmng/rmneo/user, and rmng/rmneo/user/user.go imports rmng/espuser/auth.

type User struct {
	UserID      string
	Permissions rbac.EntityPermissions
	AuthService auth.AuthService
	UserInfo    auth.UserInfo
}

func NewContextWithAPIRequest(ctx context.Context, request events.APIGatewayProxyRequest) *rmngctx.RmngContext {
	// The factory extracts the caller identity from the request and picks the resolving
	// service (OIDC user service by sub, or Cognito admin); we then resolve the user.
	authService, identity, err := auth.NewAuthServiceFactory().CreateAuthServiceFromAPIRequest(ctx, request)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to create auth service")
		return nil
	}
	if authService == nil {
		rlog.Error(ctx).Msg("auth service unavailable")
		return nil
	}
	userInfo, err := authService.GetUserFromProvider(ctx, identity)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to get user from provider")
		return nil
	}

	return rmngctx.NewRmngContextWithUser(ctx, NewUserFromRequest(userInfo.UserID, userInfo, authService), userInfo.UserID)
}

func NewUserFromRequest(userID string, userInfo auth.UserInfo, authService auth.AuthService) *User {
	u := &User{
		UserID:      userID,
		UserInfo:    userInfo,
		AuthService: authService,
	}

	u.Permissions.SetAllow(string(utils.GroupCreate), "*")
	return u
}

func NewUser(userID string) *User {
	u := &User{UserID: userID}
	u.Permissions.SetAllow(string(utils.GroupCreate), "*")
	return u
}

func NewUnconfirmedUser() *User {
	u := &User{UserID: "unconfirmed-user"}
	u.Permissions.SetAllow(string(utils.GroupCreate), "*")
	return u
}

// NewUserFromUserID fetches a user by user ID. Returns an error if the user does not exist.
// Used to resolve path parameters (e.g. try as ID first, then fall back to user code).
// Resolves against user_details (the authoritative per-user record, minted at signup),
// not the app-clients table — a user without a registered push client still exists.
func NewUserFromUserID(ctx *rmngctx.RmngContext, userID string) (*User, error) {
	details, err := user_details_db.NewUserDetailsDB(ctx).GetUserDetailsByUserID(userID)
	if err != nil {
		return nil, err
	}
	return NewUser(details.UserID), nil
}

// ErrInvalidUserName is returned when a user name is neither an email address
// nor an E.164 phone number, so there is no index to resolve it against.
var ErrInvalidUserName = errors.New("user name must be an email address or an E.164 phone number")

// NewUserFromUserName resolves a user name — the invitee's email address or
// E.164 phone number — to the user behind it. This is the identifier sharing
// speaks in: a group owner knows the person they are inviting by the name that
// person signs in with, not by their internal user ID.
//
// Returns ErrInvalidUserName if the name is neither form, and
// user_details_db.ErrUserNotFound if no account owns it. Callers must keep those
// apart: the first is the caller's mistake, the second is a lookup miss.
func NewUserFromUserName(ctx *rmngctx.RmngContext, userName string) (*User, error) {
	userName = strings.TrimSpace(userName)
	userDetailsDB := user_details_db.NewUserDetailsDB(ctx)

	var userID string
	var err error
	// Classified with the same predicates signup writes with, email first — see
	// StoreUserInDBAndCognito. Reversing the order would send an address like
	// +bob@example.com, which signup stores as an email, to the phone index instead,
	// leaving that account permanently unshareable.
	switch {
	case validation.ValidateEmail(userName):
		userID, err = userDetailsDB.LookupUserIDByEmail(userName)
	case validation.ValidatePhone(userName):
		userID, err = userDetailsDB.LookupUserIDByPhoneNumber(userName)
	default:
		return nil, ErrInvalidUserName
	}
	if err != nil {
		return nil, err
	}

	return NewUser(userID), nil
}

func (u *User) GetID() string {
	return u.UserID
}

func (u *User) GetPermissions() *rbac.EntityPermissions {
	return &u.Permissions
}

// RegisterClient writes the given UserIntegrationEntry to the rmng-user-endpoints table. Caller supplies integration_id, endpoint_id plus the appropriate credential fields for the integration's type (sns_endpoint_arn for push; access_token/refresh_token/expires_at/token_type for OAuth-style).
func (u *User) RegisterClient(ctx *rmngctx.RmngContext, entry user_integration_db.UserIntegrationEntry) error {
	return user_integration_db.NewUserDB(ctx).RegisterClient(entry)
}

func (u *User) UnregisterClient(ctx *rmngctx.RmngContext, integrationID, endpointID string) error {
	return user_integration_db.NewUserDB(ctx).UnregisterClient(integrationID, endpointID)
}

func (u *User) IsSuperAdmin(ctx *rmngctx.RmngContext) bool {
	return u.UserInfo.IsSuperAdmin
}

// LoadNodePermissions loads the node access for the user either through the group or a sub-group that the node may belong to
// For convenience, the groupID is provided, so it saves us from having to do a node lookup in all the groups
// This is typically used before performing any operation on a node, so that the appropriate permissions are loaded
func LoadNodePermissions(ctx *rmngctx.RmngContext, groupID string, nodeID string) error {
	// This loads the user's access to groups and sub-groups
	_, err := group.ListUserAccessableGroups(ctx)
	if err != nil {
		return err
	}

	// This loads the nodes for the specific groupID
	// We don't specifically check if the user has access to the group, the GetGroupNodes will internally check that for us
	// grpNodes already reflects sub-group membership: GetGroupNodes resolves it internally,
	// including replacing grpNodes with the sub-group-accessible set when access is sub-group-only.
	grpNodes, _, err := group.GetGroupNodes(ctx, groupID)
	if err != nil {
		return err
	}

	if _, ok := grpNodes[nodeID]; ok {
		return nil
	}

	return rmerror.NewRMError(nil, fmt.Sprintf("node %s is not a member of group %s", nodeID, groupID))
}

// GetUserIDFromToken resolves the user_id from a bearer access token via the token-owning
// service's GetUserFromProviderUsingToken: the OIDC user service when ESPUSER_ISSUER is
// configured (end users; RS256/JWKS, sub == user_id), else the Cognito admin service.
func GetUserIDFromToken(ctx context.Context, token string) (string, error) {
	authService, err := auth.NewAuthServiceFactory().CreateAuthServiceForToken(ctx, token)
	if err != nil {
		return "", err
	}
	userInfo, err := authService.GetUserFromProviderUsingToken(ctx, token)
	if err != nil {
		return "", err
	}
	return userInfo.UserID, nil
}

func (u *User) SetUserID(userID string) {
	// Create a new User with the ID and set it as the accessor
	// Since Accessor is a public field, we can set it directly
	u.UserID = userID
}

func (u *User) UpdateRmngCtxAccessor(rmngCtx *rmngctx.RmngContext) {
	rmngCtx.Accessor = u
}
