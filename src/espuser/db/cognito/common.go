// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package cognito

import (
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

// Custom attributes
const (
	SuperAdminFlag = "custom:super_admin"
	UserId         = "custom:user_id"
	Email          = "email"
	PhoneNumber    = "phone_number"
	Username       = "username"
)

// CognitoUserAttributes represents all user attributes from Cognito User Pool
// Based on attributes defined in esp_user_base_stack.py
type CognitoUserAttributes struct {
	Email       string
	PhoneNumber string
	UserID      string
	SuperAdmin  bool
	Username    string
}

func ParseCognitoAttributes(userAttributes []types.AttributeType) CognitoUserAttributes {
	var attrs CognitoUserAttributes

	for _, attr := range userAttributes {
		if attr.Name == nil || attr.Value == nil {
			continue
		}

		switch *attr.Name {
		case Email:
			attrs.Email = *attr.Value
		case PhoneNumber:
			attrs.PhoneNumber = *attr.Value
		case UserId:
			attrs.UserID = *attr.Value
		case SuperAdminFlag:
			attrs.SuperAdmin = *attr.Value == "true"
		case Username:
			attrs.Username = *attr.Value
		}
	}

	return attrs
}
