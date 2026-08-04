// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package awscommon

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
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
)

var (
	dynamoDBClient        DynamoDBClientInterface
	iotClient             IoTClientInterface
	iotDataPlaneClient    IoTDataPlaneClientInterface
	stsClient             STSClientInterface
	cognitoIdentityClient CognitoIdentityInterface
	cognitoProviderClient CognitoProviderInterface
	ssmClient             SSMClientInterface
	sesClient             SESClientInterface
	lambdaClient          LambdaClientInterface
	snsClient             SNSClientInterface
	s3Client              S3ClientInterface
	s3PresignClient       S3PresignClientInterface
	sQSClient             SQSClientInterface
	ecsClient             ECSClientInterface
	kvsClient             KVSClientInterface
	kmsClient             KMSClientInterface
	once                  sync.Once
)

func init() {
	once.Do(func() {
		// Initialize with real AWS clients for production.
		// Use GetRmngRegion() so when RMNG_REGION is set (e.g. alexa_skill Lambda in another region),
		// clients target the rmng deployment region for DynamoDB, IoT, SSM, etc.
		//
		// Retries: connection errors, HTTP 5xx, RequestTimeout, and the full
		// throttling 4xx other than 429 (ConditionalCheckFailed, Validation,
		// AccessDenied, ...) and ctx cancel are NOT retried.
		//
		// MaxAttempts left at the SDK default of 3 (initial + 2 retries). The
		// real throttling-smoothing win comes from adaptive mode's client-side
		// rate limiter, not from raw retry count — a 4th or 5th attempt almost
		// never recovers something the 3rd didn't, and Change 3's per-step
		// idempotency means the bulk container can safely retry an entire
		// row if needed.
		//
		// MaxBackoff is capped to 5s so retries stay well under the API
		// Gateway 29s ceiling — without the cap, exponential backoff can
		// stretch to tens of seconds and burn the whole response budget on a
		// single retried call.
		cfg, err := config.LoadDefaultConfig(context.Background(),
			config.WithRegion(GetRmngRegion()),
			config.WithRetryer(func() aws.Retryer {
				return retry.NewAdaptiveMode(func(o *retry.AdaptiveModeOptions) {
					o.StandardOptions = []func(*retry.StandardOptions){
						func(s *retry.StandardOptions) {
							s.MaxBackoff = 5 * time.Second
						},
					}
				})
			}),
		)
		if err != nil {
			panic("failed to load AWS SDK config: " + err.Error())
		}

		dynamoDBClient = dynamodb.NewFromConfig(cfg)
		iotClient = iot.NewFromConfig(cfg)
		iotDataPlaneClient = iotdataplane.NewFromConfig(cfg)
		cognitoIdentityClient = cognitoidentity.NewFromConfig(cfg)
		// stsclient is not dynamically initialized as usually it is initialised with the credentials of the user
		ssmClient = ssm.NewFromConfig(cfg)
		kmsClient = kms.NewFromConfig(cfg)
		sesClient = sesv2.NewFromConfig(cfg)
		cognitoProviderClient = cognitoidentityprovider.NewFromConfig(cfg)
		lambdaClient = lambda.NewFromConfig(cfg)
		snsClient = sns.NewFromConfig(cfg)
		s3Client = s3.NewFromConfig(cfg)
		s3PresignClient = s3.NewPresignClient(s3.NewFromConfig(cfg))
		sQSClient = sqs.NewFromConfig(cfg)
		ecsClient = ecs.NewFromConfig(cfg)
		kvsClient = kinesisvideo.NewFromConfig(cfg)
		kmsClient = kms.NewFromConfig(cfg)
	})
}

func SetCognitoIdentityClient(client CognitoIdentityInterface) {
	cognitoIdentityClient = client
}

func GetCognitoIdentityClient() CognitoIdentityInterface {
	return cognitoIdentityClient
}

func SetDynamoDBClient(client DynamoDBClientInterface) {
	dynamoDBClient = client
}

// GetDynamoDBClient returns the globally initialized DynamoDB client
func GetDynamoDBClient() DynamoDBClientInterface {
	return dynamoDBClient
}

func SetIoTClient(client IoTClientInterface) {
	iotClient = client
}

// GetIoTClient returns the globally initialized AWS IoT client
func GetIoTClient() IoTClientInterface {
	return iotClient
}

func SetIoTDataPlaneClient(client IoTDataPlaneClientInterface) {
	iotDataPlaneClient = client
}

// GetIoTDataPlaneClient returns the globally initialized AWS IoT Data Plane client
func GetIoTDataPlaneClient() IoTDataPlaneClientInterface {
	return iotDataPlaneClient
}

func SetSTSClient(client STSClientInterface) {
	stsClient = client
}

func GetSTSClient() STSClientInterface {
	return stsClient
}

func SetSSMClient(client SSMClientInterface) {
	ssmClient = client
}

func GetSSMClient() SSMClientInterface {
	return ssmClient
}

func SetKMSClient(client KMSClientInterface) {
	kmsClient = client
}

func GetKMSClient() KMSClientInterface {
	return kmsClient
}

func SetSESClient(client SESClientInterface) {
	sesClient = client
}

func GetSESClient() SESClientInterface {
	return sesClient
}

func SetCognitoProviderClient(client CognitoProviderInterface) {
	cognitoProviderClient = client
}

func GetCognitoProviderClient() CognitoProviderInterface {
	return cognitoProviderClient
}

// CognitoProviderClientForRegion returns a Cognito Identity Provider client for the given region.
// The shared client is pinned to the deployment region, but a provider whose user pool lives in
// another region needs InitiateAuth/SignUp to reach that region's endpoint. An empty region, or the
// deployment region, returns the shared client — so the test mock and same-region providers are
// unaffected; only a genuinely cross-region provider builds its own client.
func CognitoProviderClientForRegion(region string) CognitoProviderInterface {
	if region == "" || region == GetRmngRegion() {
		return GetCognitoProviderClient()
	}
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		return GetCognitoProviderClient()
	}
	return cognitoidentityprovider.NewFromConfig(cfg)
}

func GetLambdaClient() LambdaClientInterface {
	return lambdaClient
}

func SetLambdaClient(client LambdaClientInterface) {
	lambdaClient = client
}

func GetSNSClient() SNSClientInterface {
	return snsClient
}

func SetSNSClient(client SNSClientInterface) {
	snsClient = client
}

func SetS3Client(client S3ClientInterface) {
	s3Client = client
}

func GetS3Client() S3ClientInterface {
	return s3Client
}

func SetS3PresignClient(client S3PresignClientInterface) {
	s3PresignClient = client
}

func GetS3PresignClient() S3PresignClientInterface {
	return s3PresignClient
}

func SetSQSClient(client SQSClientInterface) {
	sQSClient = client
}

func GetSQSClient() SQSClientInterface {
	return sQSClient
}

func SetECSClient(client ECSClientInterface) {
	ecsClient = client
}

func GetECSClient() ECSClientInterface {
	return ecsClient
}

func SetKVSClient(client KVSClientInterface) {
	kvsClient = client
}

func GetKVSClient() KVSClientInterface {
	return kvsClient
}
