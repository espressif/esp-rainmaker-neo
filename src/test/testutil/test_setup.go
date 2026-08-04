// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/admin_config_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/assoc_request_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/automation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/nodes_online_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/processed_ts_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/sharing_request_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/timeseries_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_id_reservation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_failed_nodes_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"

	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

var TestJWKUtil = InitCognitoUtils("us-east-1")

func CreateCommonSummaryFile(filename string) (*os.File, error) {
	var err error
	output_dir := os.Getenv("TEST_OUTPUT_DIR")
	if output_dir == "" {
		output_dir = "./"
	}
	timingFile, err := os.Create(output_dir + "/" + filename)
	return timingFile, err
}

func TestSetup() {
	os.Setenv("AWS_REGION", "us-east-1")
	os.Setenv("AWS_ACCOUNT_ID", "123456789012")

	// The upstream Cognito pool a federated login authenticates against. Named UPSTREAM_
	// rather than USER_ to keep it from reading as product config: no lambda takes a pool
	// id for end users, because the broker consumes the upstream tokens server-side and
	// hands out our own. It exists here so the mock has a non-admin pool to mint against.
	os.Setenv("UPSTREAM_USER_POOL_ID", "us-east-1_TestPool")
	os.Setenv("UPSTREAM_USER_POOL_CLIENT_ID", "test-client-id")
	// The MCP server validates against the same ESP pool, and pins the app client: an
	// unset value makes it fail closed with 401 (see validateCognitoToken in
	// mcp/proxy/auth.go) before any token is inspected.
	os.Setenv("MCP_USER_POOL_CLIENT_ID", "test-client-id")
	os.Setenv("ALEXA_APP_CLIENT_ID", "vaclient")
	os.Setenv("GVA_APP_CLIENT_ID", "vaclient")
	os.Setenv("ADMIN_USER_POOL_ID", "us-east-1_TestAdminPool")
	os.Setenv("ADMIN_USER_POOL_CLIENT_ID", "test-admin-client-id")

	// Create mock clients first (needed for SSM operations)
	dynamoDBMock := mock.NewDynamoDBMock()
	awscommon.SetDynamoDBClient(dynamoDBMock)

	iotClientMock := mock.NewIoTClientMock()
	awscommon.SetIoTClient(iotClientMock)

	iotDataPlaneMock := mock.NewIoTDataPlaneMock()
	awscommon.SetIoTDataPlaneClient(iotDataPlaneMock)

	stsMock := mock.NewSTSMock()
	awscommon.SetSTSClient(stsMock)

	cognitoIdentityMock := mock.NewCognitoIdentityMock()
	awscommon.SetCognitoIdentityClient(cognitoIdentityMock)

	ssmMock := mock.NewMockSSM()
	awscommon.SetSSMClient(ssmMock)

	// The SSM parameter cache lives in SSM_-prefixed env vars (see ssmutil), so a
	// fresh mock alone does not clear it: a value cached by a previous test would
	// shadow this test's fresh store on any cached read. Clear those caches so
	// each test starts from an empty parameter store.
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "SSM_") {
			if i := strings.IndexByte(kv, '='); i > 0 {
				os.Unsetenv(kv[:i])
			}
		}
	}

	ctx := context.Background()

	// OIDC end-user signing key + JWKS + env vars (shared helper, shared key/kid across the suite).
	seedOIDCKeys(ctx, ssmMock, TestJWKUtil.SigningKey(), "https://issuer.example")

	// Cognito pool JWKS — the same shared key under each pool's own param, so a token minted for
	// either pool verifies. Both pools are needed: the end-user pool is the upstream provider the
	// password APIs authenticate against, the admin pool backs superadmin tokens.
	for envVar, paramName := range map[string]string{
		"UPSTREAM_USER_POOL_JWKS_PARA_NAME": "/esp_user/base/user_pool_jwks_json",
		"ADMIN_USER_POOL_JWKS_PARA_NAME":    "/esp_user/base/admin_user_pool_jwks_json",
	} {
		if err := ssmutil.StoreParameterWithType(ctx, paramName, string(TestJWKUtil.GetTestJWKS()), ssm_types.ParameterTypeString); err != nil {
			panic(fmt.Sprintf("failed to store %s in SSM mock: %v", paramName, err))
		}
		os.Setenv(envVar, paramName)
	}

	cognitoProviderMock := mock.NewCognitoProviderMock()
	awscommon.SetCognitoProviderClient(cognitoProviderMock)
	SetupCognitoUserPoolClients(cognitoProviderMock)

	lambdaMock := mock.NewLambdaMock()
	awscommon.SetLambdaClient(lambdaMock)

	snsMock := mock.NewSNSMock()
	awscommon.SetSNSClient(snsMock)

	httpClientMock := mock.NewMockHTTPClient()
	httpclient.Set(httpClientMock)

	s3ClientMock := mock.NewS3ClientMock()
	awscommon.SetS3Client(s3ClientMock)
	s3PresignClientMock := mock.NewS3PresignClientMock()
	awscommon.SetS3PresignClient(s3PresignClientMock)

	// Initialize the mock clients
	dynamoDBMock.AddTable(assoc_request_db.AssocRequestsTable, "request_id", "")
	dynamoDBMock.AddTable(group_db.GroupsTable, "group_id", "sub_group_id")
	dynamoDBMock.AddTable(user_group_db.UserGroupMappingTable, "user_id", "group_id")
	dynamoDBMock.AddSecondaryIndex(user_group_db.UserGroupMappingByGroupIDIndex, user_group_db.UserGroupMappingTable, "group_id", "")
	dynamoDBMock.AddTable(group_node_db.GroupDeviceMappingTable, "group_id", "node_id")
	dynamoDBMock.AddSecondaryIndex(group_node_db.GroupDeviceMappingByNodeIDIndex, group_node_db.GroupDeviceMappingTable, "node_id", "")
	dynamoDBMock.AddSecondaryIndex(group_node_db.GroupDeviceMappingByAliasIndex, group_node_db.GroupDeviceMappingTable, "alias", "")
	dynamoDBMock.AddTable(node_details_db.NodeDetailsTable, "node_id", "")
	dynamoDBMock.AddTable(sharing_request_db.SharingRequestsTable, "user_id", "sharing_request_id")
	dynamoDBMock.AddTable(user_integration_db.UserEndpointsTable, "user_id", "integration_endpoint")
	dynamoDBMock.AddTable(nodes_online_db.NodesOnlineTable, "clientId", "")
	dynamoDBMock.AddTable(automation_db.AutomationsTable, "group_id", "automation_id")
	dynamoDBMock.AddTable(user_integration_db.UserDetailsTable, "user_id", "")
	dynamoDBMock.AddSecondaryIndex(user_integration_db.UserDetailsByEmailIndex, user_integration_db.UserDetailsTable, "email", "")
	dynamoDBMock.AddSecondaryIndex("espuser-user-details-by-phone", user_integration_db.UserDetailsTable, "phone", "")
	dynamoDBMock.AddTable(node_reg_req_db.NodeRegReqsTable, "request_id", "")
	dynamoDBMock.AddTable(node_reg_failed_nodes_db.NodeRegFailedNodesTable, "request_id", "node_id")
	dynamoDBMock.AddTable(node_id_reservation_db.NodeIDReservationsTable, "claimant_id", "mac_addr")
	dynamoDBMock.AddTable(timeseries_db.RawTSDataTable, "node_key_dt", "ts")
	dynamoDBMock.AddTable(processed_ts_db.ProcessedTSDataTable, "node_key_dt", "interval_key")
	dynamoDBMock.AddTable(admin_config_db.AdminConfigTableName, "config_key", "")

	bucketName := "rmng-files-" + awscommon.GetAccountId() + "-" + awscommon.GetRmngRegion() + "-an"
	s3ClientMock.CreateBucketDirect(bucketName)
	os.Setenv("FILE_BUCKET_NAME", bucketName)

	ecsMock := mock.NewECSClientMock()
	awscommon.SetECSClient(ecsMock)
}

// SetupCognitoUserPoolClients initializes Cognito user pool clients for testing
// This is commonly needed for auth-related tests
func SetupCognitoUserPoolClients(cognitoProviderMock *mock.CognitoProviderMock) {
	// Setup user pool client
	userPoolID := os.Getenv("UPSTREAM_USER_POOL_ID")
	clientID := os.Getenv("UPSTREAM_USER_POOL_CLIENT_ID")
	cognitoProviderMock.AddTestUserPoolDirect(userPoolID, clientID)

	// Setup admin pool client
	adminUserPoolID := os.Getenv("ADMIN_USER_POOL_ID")
	adminClientID := os.Getenv("ADMIN_USER_POOL_CLIENT_ID")
	cognitoProviderMock.AddTestUserPoolDirect(adminUserPoolID, adminClientID)

	vaClientID := os.Getenv("ALEXA_APP_CLIENT_ID")
	cognitoProviderMock.AddTestUserPoolDirect(userPoolID, vaClientID)
}
