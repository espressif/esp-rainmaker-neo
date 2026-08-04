// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package snsutil

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	sns_types "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// SendSMS sends a transactional SMS to an E.164 phone number via the shared SNS client.
func SendSMS(ctx context.Context, phoneNumber, message string) error {
	_, err := awscommon.GetSNSClient().Publish(ctx, &sns.PublishInput{
		PhoneNumber: aws.String(phoneNumber),
		Message:     aws.String(message),
		MessageAttributes: map[string]sns_types.MessageAttributeValue{
			// Transactional gets the highest delivery priority (OTP must arrive promptly).
			"AWS.SNS.SMS.SMSType": {DataType: aws.String("String"), StringValue: aws.String("Transactional")},
		},
	})
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("failed to send sms to %s", phoneNumber))
	}
	return nil
}
