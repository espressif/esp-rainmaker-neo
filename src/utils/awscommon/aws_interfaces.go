// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package awscommon

import (
	"context"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentity"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/iot"
	"github.com/aws/aws-sdk-go-v2/service/iotdataplane"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type DynamoDBClientInterface interface {
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	BatchGetItem(ctx context.Context, params *dynamodb.BatchGetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error)
	BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	DescribeTable(ctx context.Context, params *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	UpdateTable(ctx context.Context, params *dynamodb.UpdateTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateTableOutput, error)
}

type IoTClientInterface interface {
	ListThingPrincipals(ctx context.Context, params *iot.ListThingPrincipalsInput, optFns ...func(*iot.Options)) (*iot.ListThingPrincipalsOutput, error)
	DescribeCertificate(ctx context.Context, params *iot.DescribeCertificateInput, optFns ...func(*iot.Options)) (*iot.DescribeCertificateOutput, error)
	RegisterCertificateWithoutCA(ctx context.Context, params *iot.RegisterCertificateWithoutCAInput, optFns ...func(*iot.Options)) (*iot.RegisterCertificateWithoutCAOutput, error)
	CreateThing(ctx context.Context, params *iot.CreateThingInput, optFns ...func(*iot.Options)) (*iot.CreateThingOutput, error)
	AttachThingPrincipal(ctx context.Context, params *iot.AttachThingPrincipalInput, optFns ...func(*iot.Options)) (*iot.AttachThingPrincipalOutput, error)
	ListThingGroups(ctx context.Context, params *iot.ListThingGroupsInput, optFns ...func(*iot.Options)) (*iot.ListThingGroupsOutput, error)
	CreateThingGroup(ctx context.Context, params *iot.CreateThingGroupInput, optFns ...func(*iot.Options)) (*iot.CreateThingGroupOutput, error)
	AddThingToThingGroup(ctx context.Context, params *iot.AddThingToThingGroupInput, optFns ...func(*iot.Options)) (*iot.AddThingToThingGroupOutput, error)
	AttachPolicy(ctx context.Context, params *iot.AttachPolicyInput, optFns ...func(*iot.Options)) (*iot.AttachPolicyOutput, error)
	DescribeThingGroup(ctx context.Context, params *iot.DescribeThingGroupInput, optFns ...func(*iot.Options)) (*iot.DescribeThingGroupOutput, error)
	DeleteCertificate(ctx context.Context, params *iot.DeleteCertificateInput, optFns ...func(*iot.Options)) (*iot.DeleteCertificateOutput, error)
	DeleteThing(ctx context.Context, params *iot.DeleteThingInput, optFns ...func(*iot.Options)) (*iot.DeleteThingOutput, error)
	DetachThingPrincipal(ctx context.Context, params *iot.DetachThingPrincipalInput, optFns ...func(*iot.Options)) (*iot.DetachThingPrincipalOutput, error)
	UpdateCertificate(ctx context.Context, params *iot.UpdateCertificateInput, optFns ...func(*iot.Options)) (*iot.UpdateCertificateOutput, error)
	UpdateThing(ctx context.Context, params *iot.UpdateThingInput, optFns ...func(*iot.Options)) (*iot.UpdateThingOutput, error)
	DescribeThing(ctx context.Context, params *iot.DescribeThingInput, optFns ...func(*iot.Options)) (*iot.DescribeThingOutput, error)
	GetTopicRule(ctx context.Context, params *iot.GetTopicRuleInput, optFns ...func(*iot.Options)) (*iot.GetTopicRuleOutput, error)
	ReplaceTopicRule(ctx context.Context, params *iot.ReplaceTopicRuleInput, optFns ...func(*iot.Options)) (*iot.ReplaceTopicRuleOutput, error)
	// GetEndpoint(ctx context.Context, params *iot.GetEndpointInput, optFns ...func(*iot.Options)) (*iot.GetEndpointOutput, error)
}

type IoTDataPlaneClientInterface interface {
	Publish(ctx context.Context, params *iotdataplane.PublishInput, optFns ...func(*iotdataplane.Options)) (*iotdataplane.PublishOutput, error)
	UpdateThingShadow(ctx context.Context, params *iotdataplane.UpdateThingShadowInput, optFns ...func(*iotdataplane.Options)) (*iotdataplane.UpdateThingShadowOutput, error)
	GetThingShadow(ctx context.Context, params *iotdataplane.GetThingShadowInput, optFns ...func(*iotdataplane.Options)) (*iotdataplane.GetThingShadowOutput, error)
	DeleteThingShadow(ctx context.Context, params *iotdataplane.DeleteThingShadowInput, optFns ...func(*iotdataplane.Options)) (*iotdataplane.DeleteThingShadowOutput, error)
}

type STSClientInterface interface {
	GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
	AssumeRole(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
}

type CognitoIdentityInterface interface {
	GetId(ctx context.Context, params *cognitoidentity.GetIdInput, optFns ...func(*cognitoidentity.Options)) (*cognitoidentity.GetIdOutput, error)
	GetCredentialsForIdentity(ctx context.Context, params *cognitoidentity.GetCredentialsForIdentityInput, optFns ...func(*cognitoidentity.Options)) (*cognitoidentity.GetCredentialsForIdentityOutput, error)
}

type CognitoProviderInterface interface {
	UpdateUserPoolClient(ctx context.Context, params *cognitoidentityprovider.UpdateUserPoolClientInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.UpdateUserPoolClientOutput, error)
	DescribeUserPoolClient(ctx context.Context, params *cognitoidentityprovider.DescribeUserPoolClientInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.DescribeUserPoolClientOutput, error)
	SignUp(ctx context.Context, params *cognitoidentityprovider.SignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.SignUpOutput, error)
	ConfirmSignUp(ctx context.Context, params *cognitoidentityprovider.ConfirmSignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmSignUpOutput, error)
	InitiateAuth(ctx context.Context, params *cognitoidentityprovider.InitiateAuthInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.InitiateAuthOutput, error)
	RespondToAuthChallenge(ctx context.Context, params *cognitoidentityprovider.RespondToAuthChallengeInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.RespondToAuthChallengeOutput, error)
	ForgotPassword(ctx context.Context, params *cognitoidentityprovider.ForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ForgotPasswordOutput, error)
	ConfirmForgotPassword(ctx context.Context, params *cognitoidentityprovider.ConfirmForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmForgotPasswordOutput, error)
	ChangePassword(ctx context.Context, params *cognitoidentityprovider.ChangePasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ChangePasswordOutput, error)
	RevokeToken(ctx context.Context, params *cognitoidentityprovider.RevokeTokenInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.RevokeTokenOutput, error)
	GetUser(ctx context.Context, params *cognitoidentityprovider.GetUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GetUserOutput, error)
	GlobalSignOut(ctx context.Context, params *cognitoidentityprovider.GlobalSignOutInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GlobalSignOutOutput, error)
	AdminGetUser(ctx context.Context, params *cognitoidentityprovider.AdminGetUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminGetUserOutput, error)
	AdminCreateUser(ctx context.Context, params *cognitoidentityprovider.AdminCreateUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminCreateUserOutput, error)
	AdminSetUserPassword(ctx context.Context, params *cognitoidentityprovider.AdminSetUserPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminSetUserPasswordOutput, error)
	AdminUpdateUserAttributes(ctx context.Context, params *cognitoidentityprovider.AdminUpdateUserAttributesInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminUpdateUserAttributesOutput, error)
	ResendConfirmationCode(ctx context.Context, params *cognitoidentityprovider.ResendConfirmationCodeInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ResendConfirmationCodeOutput, error)
}

type SSMClientInterface interface {
	PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
	GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
	DeleteParameter(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error)
}

// KMSClientInterface is the slice of KMS used for signing. Only these two calls are
// needed: the private key never leaves KMS, so there is no Decrypt/GenerateDataKey here
// and no code path that could export key material.
type KMSClientInterface interface {
	Sign(ctx context.Context, params *kms.SignInput, optFns ...func(*kms.Options)) (*kms.SignOutput, error)
	GetPublicKey(ctx context.Context, params *kms.GetPublicKeyInput, optFns ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error)
}

type SESClientInterface interface {
	SendEmail(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
	CreateEmailIdentity(ctx context.Context, params *sesv2.CreateEmailIdentityInput, optFns ...func(*sesv2.Options)) (*sesv2.CreateEmailIdentityOutput, error)
	GetEmailIdentity(ctx context.Context, params *sesv2.GetEmailIdentityInput, optFns ...func(*sesv2.Options)) (*sesv2.GetEmailIdentityOutput, error)
	ListEmailIdentities(ctx context.Context, params *sesv2.ListEmailIdentitiesInput, optFns ...func(*sesv2.Options)) (*sesv2.ListEmailIdentitiesOutput, error)
	DeleteEmailIdentity(ctx context.Context, params *sesv2.DeleteEmailIdentityInput, optFns ...func(*sesv2.Options)) (*sesv2.DeleteEmailIdentityOutput, error)
	GetAccount(ctx context.Context, params *sesv2.GetAccountInput, optFns ...func(*sesv2.Options)) (*sesv2.GetAccountOutput, error)
	PutAccountDetails(ctx context.Context, params *sesv2.PutAccountDetailsInput, optFns ...func(*sesv2.Options)) (*sesv2.PutAccountDetailsOutput, error)
}

type SNSClientInterface interface {
	Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
	CreatePlatformApplication(ctx context.Context, params *sns.CreatePlatformApplicationInput, optFns ...func(*sns.Options)) (*sns.CreatePlatformApplicationOutput, error)
	CreatePlatformEndpoint(ctx context.Context, params *sns.CreatePlatformEndpointInput, optFns ...func(*sns.Options)) (*sns.CreatePlatformEndpointOutput, error)
	DeleteEndpoint(ctx context.Context, params *sns.DeleteEndpointInput, optFns ...func(*sns.Options)) (*sns.DeleteEndpointOutput, error)
	ListPlatformApplications(ctx context.Context, params *sns.ListPlatformApplicationsInput, optFns ...func(*sns.Options)) (*sns.ListPlatformApplicationsOutput, error)
	SetPlatformApplicationAttributes(ctx context.Context, params *sns.SetPlatformApplicationAttributesInput, optFns ...func(*sns.Options)) (*sns.SetPlatformApplicationAttributesOutput, error)
	GetPlatformApplicationAttributes(ctx context.Context, params *sns.GetPlatformApplicationAttributesInput, optFns ...func(*sns.Options)) (*sns.GetPlatformApplicationAttributesOutput, error)
	DeletePlatformApplication(ctx context.Context, params *sns.DeletePlatformApplicationInput, optFns ...func(*sns.Options)) (*sns.DeletePlatformApplicationOutput, error)
}

type LambdaClientInterface interface {
	AddPermission(ctx context.Context, params *lambda.AddPermissionInput, optFns ...func(*lambda.Options)) (*lambda.AddPermissionOutput, error)
	RemovePermission(ctx context.Context, params *lambda.RemovePermissionInput, optFns ...func(*lambda.Options)) (*lambda.RemovePermissionOutput, error)
	Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)
}

type S3ClientInterface interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListBuckets(context.Context, *s3.ListBucketsInput, ...func(*s3.Options)) (*s3.ListBucketsOutput, error)
}

type S3PresignClientInterface interface {
	PresignPutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
	PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

type SQSClientInterface interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

type ECSClientInterface interface {
	RunTask(ctx context.Context, params *ecs.RunTaskInput, optFns ...func(*ecs.Options)) (*ecs.RunTaskOutput, error)
}

type KVSClientInterface interface {
	CreateSignalingChannel(ctx context.Context, params *kinesisvideo.CreateSignalingChannelInput, optFns ...func(*kinesisvideo.Options)) (*kinesisvideo.CreateSignalingChannelOutput, error)
	DescribeSignalingChannel(ctx context.Context, params *kinesisvideo.DescribeSignalingChannelInput, optFns ...func(*kinesisvideo.Options)) (*kinesisvideo.DescribeSignalingChannelOutput, error)
}
