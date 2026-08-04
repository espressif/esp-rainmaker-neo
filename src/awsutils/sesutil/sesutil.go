// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package sesutil

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	ses_types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

func SendEmail(ctx context.Context, sender, recipient, subject, body string) error {
	_, err := awscommon.GetSESClient().SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: &sender,
		Destination:      &ses_types.Destination{ToAddresses: []string{recipient}},
		Content: &ses_types.EmailContent{
			Simple: &ses_types.Message{
				Subject: &ses_types.Content{Data: &subject},
				Body:    &ses_types.Body{Text: &ses_types.Content{Data: &body}},
			},
		},
	})
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("failed to send email to %s", recipient))
	}
	return nil
}

type Identity struct {
	Email              string `json:"email"`
	VerificationStatus string `json:"verification_status"`
}

// IsVerified reports whether the identity can send (SES verification succeeded).
func (i Identity) IsVerified() bool {
	return i.VerificationStatus == string(ses_types.VerificationStatusSuccess)
}

func ListIdentities(ctx context.Context) ([]Identity, error) {
	client := awscommon.GetSESClient()
	var (
		identities []Identity
		next       *string
	)
	for {
		out, err := client.ListEmailIdentities(ctx, &sesv2.ListEmailIdentitiesInput{NextToken: next})
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to list SES identities")
		}
		for _, info := range out.EmailIdentities {
			identities = append(identities, Identity{
				Email:              aws.ToString(info.IdentityName),
				VerificationStatus: string(info.VerificationStatus),
			})
		}
		if out.NextToken == nil {
			return identities, nil
		}
		next = out.NextToken
	}
}
