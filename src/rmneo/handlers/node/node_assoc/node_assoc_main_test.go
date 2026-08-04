// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/assoc_request_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/automation_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/node_reset_handler"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/iot"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	profile    *mock.Profile
	timingFile *os.File
)

func TestNode(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Suite")
}

var _ = BeforeSuite(func() {
	var err error
	timingFile, err = test_utils.CreateCommonSummaryFile("user_node_association.txt")
	Expect(err).NotTo(HaveOccurred(), "Failed to create timing summary file")
})

func AssertRequestIdNotInDB(requestID string) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

	found := false
	dbMock.ForEachRow(assoc_request_db.AssocRequestsTable, func(item map[string]types.AttributeValue) error {
		if item["request_id"].(*types.AttributeValueMemberS).Value == requestID {
			found = true
		}
		return nil
	})
	Expect(found).To(BeFalse(), "Request ID should not be in the database")
}

func AddNodeToGroup(ctx *rmngctx.RmngContext, groupID, nodeID string) {
	_, err := group.AddNode(ctx, groupID, nodeID, nil)
	Expect(err).To(BeNil())
}

var _ = Describe("Associate Main", func() {
	var (
		ctx                      context.Context
		rmng_context             *rmngctx.RmngContext
		request                  events.APIGatewayProxyRequest
		dbMock                   *mock.DynamoDBMock
		userID                   string
		nodeID                   string
		nodeCert                 string
		rsaNodeID                string
		rsaNodeCert              string
		requestID                string
		challenge                string
		challengeResponse        string
		rsaChallenge             string
		rsaChallengeResponse     string
		invalidChallengeResponse string
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		dbMock = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

		userID = "test-user-id"
		ctx = context.Background()

		// Set up user using helper function
		newUser, _ := test_utils.SetupTestUser(ctx, userID, "test-user@example.com")
		rmng_context = rmngctx.NewRmngContext(newUser)

		nodeID = "MpRByu2UUq3n2h82iYmzio"
		nodeCert = "-----BEGIN CERTIFICATE-----\nMIIB7DCCAZKgAwIBAgIRAMagrcNPGqRhGmmQSsmwi6AwCgYIKoZIzj0EAwIwKzET\nMBEGA1UEAwwKRVNQIE1hdHRlcjEUMBIGCisGAQQBgqJ8AgEMBDEzMUIwHhcNMjQw\nODI5MDkzODQ5WhcNMjgwODI5MDkzODQ5WjBNMR8wHQYDVQQDDBZNcFJCeXUyVVVx\nM24yaDgyaVltemlvMRQwEgYKKwYBBAGConwCAQwEMTMxQjEUMBIGCisGAQQBgqJ8\nAgIMBDAwMDIwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAARa3t4+jZ1Z+M6cHg9V\nOUuVwZlklPb9aj76YE1ASvLSyQMrcQjuPKVdydEll+7m6w9iyouHUaMX8TA3jS0n\ngjUHo3UwczAOBgNVHQ8BAf8EBAMCB4AwEwYDVR0lBAwwCgYIKwYBBQUHAwIwDAYD\nVR0TAQH/BAIwADAdBgNVHQ4EFgQUWots8I8v2QgCWz8N+G+iGEiYByEwHwYDVR0j\nBBgwFoAUHB8LytM7DN3gXC1lg4gp0ZrD3lIwCgYIKoZIzj0EAwIDSAAwRQIhALgI\nP5qFd9fFGWvuEVswYdQpCxuDcGMPz6WztMskg6dDAiBk+qIX0dgoT3DvbR0lpnOo\nhlKcZjU3+YwEDU62cYl77g==\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nMIIBvDCCAWOgAwIBAgIIC6S3aD9evm4wCgYIKoZIzj0EAwIwNDEcMBoGA1UEAwwT\nRVNQIE1hdHRlciBQQUEgdGVzdDEUMBIGCisGAQQBgqJ8AgEMBDEzMUIwIBcNMjMw\nMzEwMDAwMDAwWhgPOTk5OTEyMzEyMzU5NTlaMCsxEzARBgNVBAMMCkVTUCBNYXR0\nZXIxFDASBgorBgEEAYKifAIBDAQxMzFCMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcD\nQgAEHQvGtYuLFltNTmaIaZu1VF4EmMX6ZOTzpyOd71iAARz8hkmo4zYf9AFqJoBj\n/i0thZmJ7ZQitfi7H5cc4+B1CaNmMGQwEgYDVR0TAQH/BAgwBgEB/wIBADAOBgNV\nHQ8BAf8EBAMCAQYwHQYDVR0OBBYEFBwfC8rTOwzd4FwtZYOIKdGaw95SMB8GA1Ud\nIwQYMBaAFBBXiQ7CHOd7WlZhCcoLOeraCCdxMAoGCCqGSM49BAMCA0cAMEQCIC+x\nNht5SJsdcnsCgnBOXYBqloa5zyQnRHp+3zjKGWsYAiAqipiFgrSd6348eB9vM+FQ\nojjYWhZ1AJuT2zZBXFP6Zg==\n-----END CERTIFICATE-----"
		//		nodeKey = "-----BEGIN PRIVATE KEY-----\nMIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgNhpw7AiaetiMC9tL\nzROOBWhFmpL3KRiiqp+KvkmY4PKhRANCAARa3t4+jZ1Z+M6cHg9VOUuVwZlklPb9\naj76YE1ASvLSyQMrcQjuPKVdydEll+7m6w9iyouHUaMX8TA3jS0ngjUH\n-----END PRIVATE KEY-----"
		requestID = "test-request-id"
		challenge = "260D98A96121B1D7"
		challengeResponse = "304402207fecf8134627860300b0616f96b15edef4a9b303464f5ab5a0217dd4c417aac10220188e088873f74167ca8d3de3c0a23726bb7a6589a30d080e9065289af2844ddc"
		invalidChallengeResponse = "304402207fecf8134627860300b0616f96b15edef4a9b303464f5ab5a0217dd4c417aac10220188e088873f74167ca8d3de3c0a23726bb7a6589a30d080e9065289af2844ddd"
		rmng_context.SetAllow(utils.NodeAll, nodeID)
		rmng_context.SetAllow(utils.NodeAll, rsaNodeID)

		rsaNodeID = "SLwbTfgdAYHCVURNEiyxK2"
		rsaNodeCert = "-----BEGIN CERTIFICATE-----\nMIIDRzCCAi+gAwIBAgIRAJy9bKTgKudhF7yjb/uUtU0wDQYJKoZIhvcNAQELBQAw\nVTELMAkGA1UEBhMCSU4xCzAJBgNVBAgTAk1IMQ0wCwYDVQQHEwRQdW5lMRIwEAYD\nVQQKEwlFc3ByZXNzaWYxFjAUBgNVBAMTDUVTUC1SYWluTWFrZXIwHhcNMjQwOTE2\nMDUwMjI4WhcNMjgwOTE2MDUwMjI4WjAhMR8wHQYDVQQDDBZTTHdiVGZnZEFZSENW\nVVJORWl5eEsyMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAzWZ319X5\nLlXl3ProzbxXFod/CrLR3s3yEFQAbBxEBWbLsQfP6HSqRzuoJhjtl0J/zkEeOXWS\n9eZJq5oOq164FYncvTxNHdNo093TTcNvTxZKIU2B1Z18l2LLBSy8g0gv+72RDNYY\nofMoTRP6AFYAd4ZQJDnqOv+ovByb4jJ3BhXZRI2XzJoaCYYI4jk+qTIyb2lBXpRT\nDocgeAKi0YPEiaM7I9lsRrNRJR/xIr/zYyBj/T1zYJoJ7VBi7zcvzBLWHjJniZL0\nqB6grfBtvrpcR4bycdhmI1MFUH6eAVNf5PrpF0kBAIRqS3+GjSCKk1oemWzmVBPK\nL4iyvVc7656dxQIDAQABo0YwRDAOBgNVHQ8BAf8EBAMCB4AwEwYDVR0lBAwwCgYI\nKwYBBQUHAwIwHQYDVR0OBBYEFH8WDofCR7enPNtnn5Iofqkp17rJMA0GCSqGSIb3\nDQEBCwUAA4IBAQAKrISNCifhYwc2Ea9flW0uOYp7gRw5pjI2erJxhHKwc75+3uxS\neMqS35AePXcNuKPC+vYMKNq+UOjXMic6vsMVxoB4BLzpaMXp4BI+dhxJ+Za+P7E5\nBPjKIlP/SqJJ/rovvqj8wwHBgWtBFtQPqLqB5RtJOh0wu4Vx5roe52LnwgvKTV2v\nQv1n8mD6GSHOddriVSOT3MsobqXRCNealr27oqv1wc7xqYBifHbM+rM3HVvhCB2L\nSf36Ebz4O/oLw3l0gxv1bf2z0s0WmjIGUWB1hDT4/R8Bqc2MJwhKsxleNzE6Ecl2\nDoGgYTeqI8rRpriH258k5xS7GmCfLwE95WoD\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nMIIDaDCCAlCgAwIBAgICBnUwDQYJKoZIhvcNAQELBQAwVTELMAkGA1UEBhMCSU4x\nCzAJBgNVBAgTAk1IMQ0wCwYDVQQHEwRQdW5lMRIwEAYDVQQKEwlFc3ByZXNzaWYx\nFjAUBgNVBAMTDUVTUC1SYWluTWFrZXIwHhcNMjAwNDI0MTMzNzIxWhcNMzAwNDI0\nMTMzNzIxWjBVMQswCQYDVQQGEwJJTjELMAkGA1UECBMCTUgxDTALBgNVBAcTBFB1\nbmUxEjAQBgNVBAoTCUVzcHJlc3NpZjEWMBQGA1UEAxMNRVNQLVJhaW5NYWtlcjCC\nASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAKHTyRirDV1QURE4wjpIWQyW\nVu7Qwjxvu+MkdOmec8gurN9DIPEQJOoa/pyfpuc1BceahjWUPdOxMwknKLc9dYs4\nyx/XkEOxRcY0OvtJg9Y2eAGgkuqKh+Z8DdFyAy2+VX48BaZxW4zf/a7cvGsQpffu\npNddVDJbLbK+Io0MT+tzcF9WM5ea4Hny4qBDeXXG6Uru4tnTTf/tnUqmHrVp95QT\nf9dMw+/98mEfpcQd35D9VxPwjTmZupx82AE/vvnu1m3vd1HzN/GEkdmHcvaMsNFh\nV6ucm2JTR9ocY+kBIOou3uTZmrZx6v6svKesDT3+Bmi8ncv2A71swCSjts6gDHMC\nAwEAAaNCMEAwDgYDVR0PAQH/BAQDAgKEMB0GA1UdJQQWMBQGCCsGAQUFBwMCBggr\nBgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQBnTjaZ\nIzMcL6aetdimuvWOfqrd1/Rvs3+HxFoZhU4utcV4ibg1O8MKaKejHtW3rDi+GLue\nykXDlo8UQdOEifng7WoQrKuRDuaF1dsaF4a80PBy5P4QHA9hensvkWldTZ2UqFrx\nO3sjrB+5chf4CoPEwEZ/ouKsMwFdgpFA3a7XTskwmuXQivXD8PGHXhPjLaRgAyZs\nO4psvoFW6QVXU2MRbNo2tiokQ2eVgW2t1vUdl0kjx5KMJQEdfY7ZmBFb+XL6goMD\nMIyP0BJg/V3WjuGYY3aWlkaob3TbBlePQkzAMtZlBtOjQsiwGBafIeOZcm2rN3Xv\nzy4NdNX/isyg/1C4\n-----END CERTIFICATE-----"
		rsaChallenge = "tzRLYOpLMEllEdtJEiDWBCPYkOcrxLFwRxJWGQEiuqrpKLrYhNZkbCuUALOJRaoc"
		rsaChallengeResponse = "3069d8c257faa42f8d4f569487b35f36658c81db6b57feba6551608b5662d8889e2927e1d4699d39cfa8cc7af608ffe7d892b9b647828860989b53f2a5418633e63a204f30f3f5eea731f4bbf87522312ae371afb921a922b5bee54f7c054f83b6998779f53953268d5e392a604015cdedda068878de93db86e17c418f4dd2580ad1322176343359a28a6de8890e141a9b34e6c3dabdc048007b8b312ae76d79e983bb7c382cc25bf58634a368e3a8931474b8eadbbac80dc7e1504ad38ea22a3ed03a8279008f907aa3aa74bd3b488b085a1a889baedc88c065b50db0753e979d2847f45f55d333e2a00e0cc8c7a4dab5df06f4c6142488dbd6242c04e3e887"
		request = events.APIGatewayProxyRequest{
			HTTPMethod: "POST",
			Path:       "/v1/groups/test-group-id/node-assoc-requests",
			Body:       `{}`,
			PathParameters: map[string]string{
				"groupId": "test-group-id",
			},
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             userID,
					CognitoAuthenticationProvider: "https://issuer.example:" + userID,
				},
			},
		}

		// Assign the mock function to the variable
		ids.GenerateRequestId = func() string {
			return requestID
		}
		ids.GenerateChallenge = func() string {
			return challenge
		}
	})

	Describe("handleInitiate", func() {
		It("should successfully initiate the association process", func() {
			// Create a group for testing
			newUser := user.NewUser(userID)
			rmng_ctx := rmngctx.NewRmngContext(newUser)
			testGroupName := "test-initiate-group"
			testGroup, err := group.CreateGroupForUser(rmng_ctx, testGroupName)
			Expect(err).To(BeNil())

			// Set up request with path parameters
			request.PathParameters = map[string]string{"groupId": testGroup.GroupID}
			request.Resource = "/v1/groups/{groupId}/node-assoc-requests"

			response, err := handleInitiate(ctx, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			var responseBody map[string]string
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
			Expect(responseBody["request_id"]).To(Equal(requestID))
			Expect(responseBody["challenge"]).To(Equal(challenge))

			// Verify that the data was stored in DynamoDB
			getItemInput := &dynamodb.GetItemInput{
				TableName: aws.String(assoc_request_db.AssocRequestsTable),
				Key: map[string]types.AttributeValue{
					"request_id": &types.AttributeValueMemberS{Value: requestID},
				},
			}
			result, err := dbMock.GetItem(ctx, getItemInput)
			Expect(err).To(BeNil())
			Expect(result.Item["challenge"].(*types.AttributeValueMemberS).Value).To(Equal(challenge))
			Expect(result.Item["user_id"].(*types.AttributeValueMemberS).Value).To(Equal(userID))
			Expect(result.Item["group_id"].(*types.AttributeValueMemberS).Value).To(Equal(testGroup.GroupID))
		})
	})
	Describe("handleVerify", func() {
		var (
			verifyRequest              events.APIGatewayProxyRequest
			new_group_id, old_group_id string
		)

		BeforeEach(func() {
			iotClientMock := mock.NewIoTClientMock()
			awscommon.SetIoTClient(iotClientMock)

			var err error
			newUser := user.NewUser(userID)
			rmng_context := rmngctx.NewRmngContext(newUser)

			new_group_name := "new-group-name"
			group.CreateGroupForUser(rmng_context, new_group_name)
			new_group_id, err = test_utils.GetGroupIDFromName(new_group_name)
			Expect(err).To(BeNil())

			old_group_name := "old-group-name"
			group.CreateGroupForUser(rmng_context, old_group_name)
			old_group_id, err = test_utils.GetGroupIDFromName(old_group_name)
			Expect(err).To(BeNil())

			verifyRequest = events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"groupId":   new_group_id,
					"requestId": requestID,
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				Body: `{"challenge_response":"` + challengeResponse + `","node_id":"` + nodeID + `"}`,
			}

			nodeIDRegistered, err := node.RegisterNodeInRmng(rmng_context, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
			Expect(err).To(BeNil())
			Expect(nodeIDRegistered).To(Equal(nodeID))

			rsaNodeIDRegistered, err := node.RegisterNodeInRmng(rmng_context, rsaNodeCert, "", []string{}, []string{}, "test-user-id", nil)
			Expect(err).To(BeNil())
			Expect(rsaNodeIDRegistered).To(Equal(rsaNodeID))
		})

		It("should successfully verify the challenge response and add node to group for ECDSA", func() {
			// Store the request data in DynamoDB mock (including group_id for verify)
			storeInDynamoDB(ctx, requestID, challenge, userID, new_group_id)

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).To(ContainSubstring("success"))

			// Verify that the requestId was deleted from DynamoDB
			AssertRequestIdNotInDB(requestID)

			// Verify that the node was added to the group, tagged "rmng".
			test_utils.AssertNodeInGroup(new_group_id, nodeID, "rmng")
		})

		It("should successfully verify the challenge response and add node to group for RSA", func() {
			// Store the request data in DynamoDB mock (including group_id)
			storeInDynamoDB(ctx, requestID, rsaChallenge, userID, new_group_id)

			verifyRequest.Body = `{"challenge_response":"` + rsaChallengeResponse + `","node_id":"` + rsaNodeID + `"}`
			dbMock.ProfileReset()

			response, err := handleVerify(ctx, verifyRequest)

			p := dbMock.ProfileGet()
			profile = &p
			readCnt, writeCnt := profile.TotalCounts()
			Expect(readCnt).To(Equal(4))
			Expect(writeCnt).To(Equal(2))

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).To(ContainSubstring("success"))

			// Verify that the requestId was deleted from DynamoDB
			AssertRequestIdNotInDB(requestID)

			// Verify that the node was added to the new group, tagged "rmng".
			test_utils.AssertNodeInGroup(new_group_id, rsaNodeID, "rmng")
		})

		It("should return an error for invalid request body", func() {
			storeInDynamoDB(ctx, requestID, challenge, userID, new_group_id)
			rmng_context.SetAllow(utils.GroupAll, old_group_id)
			AddNodeToGroup(rmng_context, old_group_id, nodeID)
			test_utils.AssertNodeInGroup(old_group_id, nodeID)

			verifyRequest.Body = "invalid json"
			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid request body"))

			// Verify that the node is part of the old group
			test_utils.AssertNodeInGroup(old_group_id, nodeID)
		})

		It("should return an error for invalid request ID", func() {
			verifyRequest.PathParameters["requestId"] = "invalid-id"
			verifyRequest.Body = `{"challenge_response":"` + challengeResponse + `","node_id":"` + nodeID + `"}`
			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid request ID"))
		})

		It("should return an error when group ID does not match stored value", func() {
			storeInDynamoDB(ctx, requestID, challenge, userID, new_group_id)
			verifyRequest.PathParameters["groupId"] = "wrong-group-id"

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Group ID mismatch"))
		})

		It("should return an error when user ID doesn't match", func() {
			// Store the request data with a different user ID (group_id must match path for other checks to run)
			storeInDynamoDB(ctx, requestID, challenge, "different-user-id", new_group_id)

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(response.Body).To(ContainSubstring("User ID mismatch"))
		})

		It("should return an error when group ID doesn't match stored group ID", func() {
			// Store the request data with one group_id
			storeInDynamoDB(ctx, requestID, challenge, userID, new_group_id)

			// Try to verify with a different group_id in the path
			verifyRequest.PathParameters["groupId"] = "wrong-group-id"
			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Group ID mismatch"))
		})

		It("should return an error for invalid challenge response", func() {
			// Store the request data in DynamoDB mock (including group_id)
			storeInDynamoDB(ctx, requestID, challenge, userID, new_group_id)
			rmng_context.SetAllow(utils.GroupAll, old_group_id)
			AddNodeToGroup(rmng_context, old_group_id, nodeID)
			test_utils.AssertNodeInGroup(old_group_id, nodeID)

			verifyRequest.Body = `{"challenge_response":"` + invalidChallengeResponse + `","node_id":"` + nodeID + `"}`
			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(response.Body).To(ContainSubstring("Invalid challenge response"))

			// Verify that the item was deleted from DynamoDB
			getItemInput := &dynamodb.GetItemInput{
				TableName: aws.String(assoc_request_db.AssocRequestsTable),
				Key: map[string]types.AttributeValue{
					"request_id": &types.AttributeValueMemberS{Value: requestID},
				},
			}
			result, err := dbMock.GetItem(ctx, getItemInput)
			Expect(err).To(BeNil())
			Expect(result.Item).To(BeNil())

			// Verify that the node is part of the old group
			test_utils.AssertNodeInGroup(old_group_id, nodeID)
		})

		It("should not add a node to a group that the user doesn't own", func() {
			// Store the request data in DynamoDB mock (with group_id = otherGroup so path matches)
			otherUserID := "other-user-id"

			// Set up other user using helper function
			_, otherUserContext := test_utils.SetupTestUser(ctx, otherUserID, "other-user@example.com")
			groupName := "Other User's Group"
			otherGroup, err := group.CreateGroupForUser(otherUserContext, groupName)
			Expect(err).To(BeNil())
			storeInDynamoDB(ctx, requestID, challenge, userID, otherGroup.GroupID)

			// Store the request data in DynamoDB mock with the other user's group_id
			storeInDynamoDB(ctx, requestID, challenge, userID, otherGroup.GroupID)

			verifyRequest := events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"groupId":   otherGroup.GroupID,
					"requestId": requestID,
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				Body: `{"challenge_response":"` + challengeResponse + `","node_id":"` + nodeID + `"}`,
			}

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("{\"message\":\"Failed to confirm node association\"}"))

			// Verify that the requestId was deleted from DynamoDB
			AssertRequestIdNotInDB(requestID)

			// Verify that the node was not added to the group
			test_utils.AssertNodeNotInGroup(otherGroup.GroupID, nodeID)
		})

		It("should move the node from an older group not owned by the current user", func() {
			// Enable node_data_reset Lambda invocation
			os.Setenv("NODE_DATA_RESET_FUNCTION_NAME", "test-node-data-reset")
			defer os.Unsetenv("NODE_DATA_RESET_FUNCTION_NAME")
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			lambdaMock.InvokeCalls = nil

			// Create a group owned by a different user
			otherUserID := "other-user-id"

			// Set up other user using helper function
			_, otherUserContext := test_utils.SetupTestUser(ctx, otherUserID, "other-user@example.com")
			oldGroupName := "Other User's Group"
			oldGroup, err := group.CreateGroupForUser(otherUserContext, oldGroupName)
			Expect(err).To(BeNil())
			otherUserContext.SetAllow(utils.NodeAll, nodeID)

			// Add the node to the other user's group
			AddNodeToGroup(otherUserContext, oldGroup.GroupID, nodeID)
			test_utils.AssertNodeInGroup(oldGroup.GroupID, nodeID)

			// Create a new group for the current user
			newGroupName := "Current User's Group"
			newGroup, err := group.CreateGroupForUser(rmng_context, newGroupName)
			Expect(err).To(BeNil())

			// Store the request data in DynamoDB mock (group_id = new group for verify)
			storeInDynamoDB(ctx, requestID, challenge, userID, newGroup.GroupID)

			// Prepare the verify request
			verifyRequest := events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"groupId":   newGroup.GroupID,
					"requestId": requestID,
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				Body: `{"challenge_response":"` + challengeResponse + `","node_id":"` + nodeID + `"}`,
			}

			// Execute the verify request
			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).To(ContainSubstring("success"))

			// Verify that the requestId was deleted from DynamoDB
			AssertRequestIdNotInDB(requestID)

			// Verify that the node was moved to the new group
			test_utils.AssertNodeInGroup(newGroup.GroupID, nodeID, "rmng")

			// Verify that the node was removed from the old group
			test_utils.AssertNodeNotInGroup(oldGroup.GroupID, nodeID)

			// Verify node_data_reset Lambda was invoked for the old group
			test_utils.AssertNodeDataResetInvoked("test-node-data-reset", nodeID, oldGroup.GroupID)
		})

		It("should clean up old group data via node_data_reset during cross-user reassociation", func() {
			// This test verifies that the permissions passed to node_data_reset
			// are sufficient to clean up automations in the old group, even though
			// the new user has no permissions on the old group.
			os.Setenv("NODE_DATA_RESET_FUNCTION_NAME", "test-node-data-reset")
			defer os.Unsetenv("NODE_DATA_RESET_FUNCTION_NAME")
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			lambdaMock.InvokeCalls = nil

			// Register the real node_data_reset handler so the mock Lambda executes it
			mock.LambdaHandlerMap["test-node-data-reset"] = func(ctx context.Context, payload []byte) ([]byte, error) {
				var event node.NodeDataResetEvent
				if err := json.Unmarshal(payload, &event); err != nil {
					return nil, err
				}
				err := node_reset_handler.HandleNodeDataReset(ctx, event)
				return nil, err
			}
			defer delete(mock.LambdaHandlerMap, "test-node-data-reset")

			// Create a group owned by a different user
			otherUserID := "other-user-cleanup"
			_, otherUserContext := test_utils.SetupTestUser(ctx, otherUserID, "other-cleanup@example.com")
			oldGroup, err := group.CreateGroupForUser(otherUserContext, "Old User Group")
			Expect(err).To(BeNil())
			otherUserContext.SetAllow(utils.NodeAll, nodeID)

			// Add the node to the other user's group
			AddNodeToGroup(otherUserContext, oldGroup.GroupID, nodeID)
			test_utils.AssertNodeInGroup(oldGroup.GroupID, nodeID)

			// Seed an automation referencing the node in the old group
			autoItem := automation_db.AutomationItem{
				GroupID:      oldGroup.GroupID,
				AutomationID: "cleanup-auto",
				Payload: map[string]interface{}{
					"name":       "Auto to clean up",
					"conditions": map[string]interface{}{"and": []interface{}{nodeID + "~cleanup-auto~0"}},
					"actions":    map[string]interface{}{"targets": []interface{}{map[string]interface{}{"node": nodeID, "path": "Light.Power", "value": true}}},
				},
			}
			item, marshalErr := attributevalue.MarshalMap(autoItem)
			Expect(marshalErr).To(BeNil())
			dbMock.PutItem(ctx, &dynamodb.PutItemInput{
				TableName: aws.String(automation_db.AutomationsTable),
				Item:      item,
			})
			test_utils.AssertAutomationExists(oldGroup.GroupID, "cleanup-auto")

			// Create a new group for the current user
			newGroup, err := group.CreateGroupForUser(rmng_context, "New User Group")
			Expect(err).To(BeNil())

			storeInDynamoDB(ctx, requestID, challenge, userID, newGroup.GroupID)

			verifyRequest := events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"groupId":   newGroup.GroupID,
					"requestId": requestID,
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				Body: `{"challenge_response":"` + challengeResponse + `","node_id":"` + nodeID + `"}`,
			}

			response, err := handleVerify(ctx, verifyRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify the node moved to the new group
			test_utils.AssertNodeInGroup(newGroup.GroupID, nodeID, "rmng")
			test_utils.AssertNodeNotInGroup(oldGroup.GroupID, nodeID)

			// Verify the automation in the old group was cleaned up by node_data_reset
			test_utils.AssertAutomationNotExists(oldGroup.GroupID, "cleanup-auto")
		})

		It("should return error when both challenge_response and nocsr_elements are provided (mutual exclusivity)", func() {
			storeInDynamoDB(ctx, requestID, challenge, userID, new_group_id)

			// Build request with BOTH challenge_response and nocsr_elements
			verifyRequest.Body = `{"challenge_response":"` + challengeResponse + `","node_id":"` + nodeID + `","nocsr_elements":"1234abcd"}`
			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("challenge_response and nocsr_elements are mutually exclusive"))
		})

		It("should return error when neither challenge_response nor nocsr_elements is provided", func() {
			storeInDynamoDB(ctx, requestID, challenge, userID, new_group_id)

			// Build request with NEITHER challenge_response nor nocsr_elements
			verifyRequest.Body = `{"node_id":"` + nodeID + `"}`
			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Either challenge_response or nocsr_elements is required"))
		})

		It("should return error when nocsr_elements used on non-Matter group", func() {
			// Store non-Matter group request
			storeInDynamoDB(ctx, requestID, challenge, userID, new_group_id)

			// Generate test CSR and build NOCSRElements
			csrDER, _, _ := generateTestCSR()
			csrNonce := make([]byte, 32)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, nil)

			// Build request with nocsr_elements for non-Matter group
			requestBodyMap := map[string]string{
				"nocsr_elements":        hex.EncodeToString(nocsrElements),
				"attestation_challenge": hex.EncodeToString(make([]byte, 16)),
				"attestation_signature": hex.EncodeToString(make([]byte, 64)),
			}
			requestBodyJSON, _ := json.Marshal(requestBodyMap)
			verifyRequest.Body = string(requestBodyJSON)

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("nocsr_elements is only valid for Matter-capable groups"))
		})

		It("should return error when challenge_response provided without node_id", func() {
			storeInDynamoDB(ctx, requestID, challenge, userID, new_group_id)

			// Build request with challenge_response but NO node_id
			verifyRequest.Body = `{"challenge_response":"` + challengeResponse + `"}`
			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("node_id is required when using challenge_response"))
		})

		It("should successfully verify challenge_response on Matter group and add node to group", func() {
			// Create a Matter group
			opts := &group.CreateGroupOptions{
				Capabilities: []string{"matter"},
			}
			matterGroup, err := group.CreateGroupForUserWithOptions(rmng_context, "Test Matter Challenge Response Group", opts)
			Expect(err).To(BeNil())

			// Store the request data in DynamoDB as a Matter group request
			storeInDynamoDBMatter(ctx, requestID, challenge, userID, matterGroup.GroupID)

			// Build request with challenge_response (not nocsr_elements)
			verifyRequest := events.APIGatewayProxyRequest{
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{
					"groupId":   matterGroup.GroupID,
					"requestId": requestID,
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				Body:     `{"challenge_response":"` + challengeResponse + `","node_id":"` + nodeID + `"}`,
			}

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).To(ContainSubstring("success"))

			// Verify that the requestId was deleted from DynamoDB (immediate confirmation like non-Matter)
			AssertRequestIdNotInDB(requestID)

			// A challenge_response node on a Matter group is still a plain RainMaker node: "rmng".
			test_utils.AssertNodeInGroup(matterGroup.GroupID, nodeID, "rmng")
		})
	})
})

// storeInDynamoDB is a backward-compatible wrapper for non-Matter groups
func storeInDynamoDB(ctx context.Context, requestID, challenge, userID, groupID string) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	assocDB := assoc_request_db.NewAssocRequestDB(rmngCtx)
	return assocDB.StoreAssocRequest(&assoc_request_db.AssocRequestEntry{
		RequestID:     requestID,
		Challenge:     challenge,
		UserID:        userID,
		GroupID:       groupID,
		IsMatterGroup: false,
		Status:        assoc_request_db.AssocStatusPending,
	})
}

// storeInDynamoDBMatter stores a Matter group association request
func storeInDynamoDBMatter(ctx context.Context, requestID, challenge, userID, groupID string) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	assocDB := assoc_request_db.NewAssocRequestDB(rmngCtx)
	return assocDB.StoreAssocRequest(&assoc_request_db.AssocRequestEntry{
		RequestID:     requestID,
		Challenge:     challenge,
		UserID:        userID,
		GroupID:       groupID,
		IsMatterGroup: true,
		Status:        assoc_request_db.AssocStatusPending,
	})
}

// expireAssocRequest rewrites the stored request with a past expiry, standing in for a row whose DynamoDB TTL sweep has not run yet.
func expireAssocRequest(ctx context.Context, requestID string) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	entry, err := assoc_request_db.NewAssocRequestDB(rmngCtx).GetAssocRequestByID(requestID)
	if err != nil {
		return err
	}
	entry.ExpirationTime = time.Now().Add(-time.Minute).Unix()
	item, err := attributevalue.MarshalMap(entry)
	if err != nil {
		return err
	}
	_, err = awscommon.GetDynamoDBClient().PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(assoc_request_db.AssocRequestsTable),
		Item:      item,
	})
	return err
}

func matterVerifyRequest(groupID, requestID, userID string, body map[string]string) events.APIGatewayProxyRequest {
	bodyJSON, _ := json.Marshal(body)
	return events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"groupId": groupID, "requestId": requestID},
		Resource:       "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
		Body: string(bodyJSON),
	}
}

// generateTestCSRWithCurve builds a CSR on an arbitrary curve, for exercising curves Matter forbids.
func generateTestCSRWithCurve(curve elliptic.Curve) ([]byte, error) {
	privateKey, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return nil, err
	}
	template := &x509.CertificateRequest{Subject: pkix.Name{CommonName: "Test Device"}}
	return x509.CreateCertificateRequest(rand.Reader, template, privateKey)
}

// buildTestNOCSRElementsTLV builds a valid NOCSRElements TLV structure for testing
func buildTestNOCSRElementsTLV(csr, csrNonce, vendorReserved1 []byte) []byte {
	var buf bytes.Buffer

	// Structure start (0x15)
	buf.WriteByte(0x15)

	// CSR (tag 0x01) with 2-byte length
	if csr != nil {
		buf.WriteByte(0x31)                         // Context-specific tag (0x20) + 2-byte octet string (0x11)
		buf.WriteByte(0x01)                         // Tag value
		buf.WriteByte(byte(len(csr) & 0xFF))        // Length low byte
		buf.WriteByte(byte((len(csr) >> 8) & 0xFF)) // Length high byte
		buf.Write(csr)
	}

	// CSRNonce (tag 0x02) with 1-byte length
	if csrNonce != nil {
		buf.WriteByte(0x30) // Context-specific tag (0x20) + 1-byte octet string (0x10)
		buf.WriteByte(0x02) // Tag value
		buf.WriteByte(byte(len(csrNonce)))
		buf.Write(csrNonce)
	}

	// VendorReserved1 (tag 0x03) with 1-byte length
	if vendorReserved1 != nil {
		buf.WriteByte(0x30) // Context-specific tag (0x20) + 1-byte octet string (0x10)
		buf.WriteByte(0x03) // Tag value
		buf.WriteByte(byte(len(vendorReserved1)))
		buf.Write(vendorReserved1)
	}

	// Structure end (0x18)
	buf.WriteByte(0x18)

	return buf.Bytes()
}

// generateTestCSR creates a valid DER-encoded CSR for testing
func generateTestCSR() ([]byte, *ecdsa.PrivateKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: "Test Device",
		},
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, privateKey)
	if err != nil {
		return nil, nil, err
	}

	return csrDER, privateKey, nil
}

// generateTestCertificate creates a self-signed certificate for testing
func generateTestCertificate(nodeID string) (string, *ecdsa.PrivateKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", nil, err
	}

	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: nodeID,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return string(certPEM), privateKey, nil
}

// base64EncodeWithLineBreaks encodes bytes to base64 with line breaks every 64 characters (PEM format)
func base64EncodeWithLineBreaks(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	var result strings.Builder
	for i := 0; i < len(encoded); i += 64 {
		end := i + 64
		if end > len(encoded) {
			end = len(encoded)
		}
		result.WriteString(encoded[i:end])
		if end < len(encoded) {
			result.WriteString("\n")
		}
	}
	return result.String()
}

// signAttestationData signs the TBS data (NOCSRElements || AttestationChallenge) with the given private key
func signAttestationData(nocsrElements, attestationChallenge []byte, privateKey *ecdsa.PrivateKey) ([]byte, error) {
	tbs := append(nocsrElements, attestationChallenge...)
	hash := sha256.Sum256(tbs)

	r, s, err := ecdsa.Sign(rand.Reader, privateKey, hash[:])
	if err != nil {
		return nil, err
	}

	// Convert to raw r||s format (64 bytes for P-256)
	rawSig := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(rawSig[32-len(rBytes):32], rBytes)
	copy(rawSig[64-len(sBytes):64], sBytes)

	return rawSig, nil
}

var _ = Describe("Matter Attestation Verification", func() {
	var (
		ctx           context.Context
		rmng_context  *rmngctx.RmngContext
		userID        string
		iotClientMock *mock.IoTClientMock
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		userID = "test-user-id"
		ctx = context.Background()

		newUser, _ := test_utils.SetupTestUser(ctx, userID, "test-user@example.com")
		rmng_context = rmngctx.NewRmngContext(newUser)

		iotClientMock = mock.NewIoTClientMock()
		awscommon.SetIoTClient(iotClientMock)
	})

	Describe("verifyMatterAttestation", func() {
		// Test vectors from verify_op_csr.py - known working artifacts from Matter commissioning
		It("should verify attestation using known-good test vectors from Python script via handleMatterVerify", func() {
			// NOCSRElements from DUT (254 bytes) - contains CSR + CSRNonce + vendor_reserved1
			// Exact hex from verify_op_csr.py with spaces removed
			nocsrElementsHex := "153001CB3081C8307002010030" +
				"0E310C300A060355040A0C03435352305930130607" +
				"2A8648CE3D020106082A8648CE3D03010703420004" +
				"A6D9549D9CC3B139615578468A23ACC35FA10090B9" +
				"08919899D61D3C98755FCFFC7531F86DFAFFD1666D" +
				"AE590187E1FF7BD592723E0508CFC51CDE1CF5B949" +
				"EFA000300A06082A8648CE3D040302034800304502" +
				"2100F19583CD6A94AD34E51FFFC323DDD932F277E8" +
				"0159CFE041B9F6F168255AFF0B022055ADCC02D892" +
				"144CFDCD9E2F1B19B698F45BE5ABF817F30D190BAB" +
				"1E47E0AF1E3002203D54C231AF36C1979E120D2570" +
				"38A83B07584A6B2EC3652BED70FFE96965892930" +
				"0308CDAB57130000000018"

			// AttestationChallenge from PASE/CASE session (16 bytes)
			attestationChallengeHex := "0C7F62C5656A405EF092B4402E870934"

			// CSR Attestation Signature from DUT (64 bytes - raw r||s format)
			csrSignatureHex := "62B1D5C7F34648217AB73A3EFA329E111F208D9760E37F8227D2F9842D0C255B" +
				"17D3C6E872DE3360D3821FD58D3FC67AA68BB1984D4A972A5EE451D59E6B8813"

			// DAC Certificate (493 bytes DER) - contains public key for verification
			// Exact hex from verify_op_csr.py with spaces removed
			dacCertDERHex := "308201E930820" +
				"18EA003020102020823" +
				"8A647BBC4C30DD300A06082A8648CE3D04030230" +
				"3D3125302306035504030C1C4D617474657220446576205041492030" +
				"784646463120" +
				"6E6F205049443114301206" +
				"0A2B0601040182A27C02010C044646463130201" +
				"70D3232303230353030303030305A180F393939" +
				"39313233313233353935395A30533125302306" +
				"035504030C1C4D617474657220446576204441" +
				"43203078464646312F30783830303031143012" +
				"060A2B0601040182A27C02010C044646463131" +
				"1430" +
				"12060A2B0601040182A27C02020C0438303030" +
				"3059301306072A8648CE3D020106082A8648CE" +
				"3D0301070342000462DB16BADEA326A6DB8481" +
				"4A063FC6C7E9E2B101B721648EBA4E5AC840F5" +
				"DA301EE618124EB4180E2FC3A2047A564BA9BC" +
				"FA0BF71F60CE8930F1E7F66EC8D728A360305E" +
				"300C0603551D130101FF04023000300E060355" +
				"1D0F0101FF040403020780301D0603551D0E04" +
				"160414BCF7B0074970636" +
				"06A26BE4E087C5956877" +
				"45A5A301F0603551D23041830168014" +
				"63540E47F64B1C38D13884A462D16C195D8FFB" +
				"3C300A06082A8648CE3D040302034900304602" +
				"2100979711EC9E7618CE41801132C250DB7076" +
				"74630CD58C12C6E2315F08D01EE17802210" +
				"0ECFC1306BD2A133D122A278610EA3DCA47F05C" +
				"7A8B805FA71C6FF41538A864C8"

			// Decode the test vectors
			nocsrElements, err := hex.DecodeString(nocsrElementsHex)
			Expect(err).To(BeNil())

			dacCertDER, err := hex.DecodeString(dacCertDERHex)
			Expect(err).To(BeNil())

			// Parse NOCSRElements to extract vendor_reserved1 (the nodeID)
			fields, err := group.ParseNOCSRElements(nocsrElements)
			Expect(err).To(BeNil())
			Expect(fields.VendorReserved1).NotTo(BeNil())

			// The nodeID is the vendor_reserved1 field as a string
			nodeID := string(fields.VendorReserved1)

			// Convert DAC DER certificate to PEM format
			dacCertPEM := "-----BEGIN CERTIFICATE-----\n" +
				base64EncodeWithLineBreaks(dacCertDER) +
				"\n-----END CERTIFICATE-----"

			// Set up the mock: create thing and register certificate
			// 1. Create the thing in IoT Core mock
			_, err = iotClientMock.CreateThing(ctx, &iot.CreateThingInput{
				ThingName: aws.String(nodeID),
			})
			Expect(err).To(BeNil())

			// 2. Register the DAC certificate
			certOutput, err := iotClientMock.RegisterCertificateWithoutCA(ctx, &iot.RegisterCertificateWithoutCAInput{
				CertificatePem: aws.String(dacCertPEM),
				Status:         "ACTIVE",
			})
			Expect(err).To(BeNil())

			// 3. Attach certificate to the thing
			_, err = iotClientMock.AttachThingPrincipal(ctx, &iot.AttachThingPrincipalInput{
				ThingName: aws.String(nodeID),
				Principal: certOutput.CertificateArn,
			})
			Expect(err).To(BeNil())

			// 4. Create a Matter group
			opts := &group.CreateGroupOptions{
				Capabilities: []string{"matter"},
			}
			matterGroup, err := group.CreateGroupForUserWithOptions(rmng_context, "Test Known-Good Attestation Group", opts)
			Expect(err).To(BeNil())

			// 5. Store assoc request in DynamoDB
			// The CSRNonce from the nocsrElementsHex is at tag 0x02: 3D54C231AF36C1979E120D257038A83B07584A6B2EC3652BED70FFE969658929
			csrNonceHex := "3D54C231AF36C1979E120D257038A83B07584A6B2EC3652BED70FFE969658929"
			storeInDynamoDBMatter(ctx, "test-known-good-req", csrNonceHex, userID, matterGroup.GroupID)

			// 6. Build request and call handleVerify (instead of handleMatterVerify directly)
			requestBody := map[string]string{
				"nocsr_elements":        nocsrElementsHex,
				"attestation_challenge": attestationChallengeHex,
				"attestation_signature": csrSignatureHex,
				// NO challenge_response or node_id - using nocsr_elements flow
			}
			requestBodyJSON, err := json.Marshal(requestBody)
			Expect(err).To(BeNil())

			verifyRequest := events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"groupId":   matterGroup.GroupID,
					"requestId": "test-known-good-req",
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				Body: string(requestBodyJSON),
			}

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK), "Expected 200 OK but got %d: %s", response.StatusCode, response.Body)

			// Verify the response contains the NOC
			var responseBody map[string]string
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
			Expect(responseBody["message"]).To(Equal("success"))
			Expect(responseBody["noc"]).To(ContainSubstring("-----BEGIN CERTIFICATE-----"))
			Expect(responseBody["matter_node_id"]).NotTo(BeEmpty())
		})

		It("should successfully verify attestation with matching certificate", func() {
			// Generate test CSR
			csrDER, _, err := generateTestCSR()
			Expect(err).To(BeNil())

			// Create test node with certificate
			testNodeID := "TestAttestationNode123"
			certPEM, dacPrivateKey, err := generateTestCertificate(testNodeID)
			Expect(err).To(BeNil())

			// Register the node with this certificate
			_, err = node.RegisterNodeInRmng(rmng_context, certPEM, "", []string{}, []string{}, userID, nil)
			Expect(err).To(BeNil())

			// Build NOCSRElements TLV with vendor_reserved1 containing nodeID
			csrNonce := make([]byte, 32)
			rand.Read(csrNonce)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, []byte(testNodeID))

			// Generate attestation challenge (16 bytes)
			attestationChallenge := make([]byte, 16)
			rand.Read(attestationChallenge)

			// Sign with DAC private key
			attestationSignature, err := signAttestationData(nocsrElements, attestationChallenge, dacPrivateKey)
			Expect(err).To(BeNil())

			// Verify attestation
			input := &MatterAttestationInput{
				NOCSRElements:        nocsrElements,
				AttestationChallenge: attestationChallenge,
				AttestationSignature: attestationSignature,
			}

			result, err := verifyMatterAttestation(ctx, input)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.CSR).To(Equal(csrDER))
			Expect(result.NodeID).To(Equal(testNodeID))
		})

		It("should fail verification with wrong signature", func() {
			// Generate test CSR
			csrDER, _, err := generateTestCSR()
			Expect(err).To(BeNil())

			// Create test node with certificate
			testNodeID := "TestWrongSigNode456"
			certPEM, _, err := generateTestCertificate(testNodeID)
			Expect(err).To(BeNil())

			// Register the node with this certificate
			_, err = node.RegisterNodeInRmng(rmng_context, certPEM, "", []string{}, []string{}, userID, nil)
			Expect(err).To(BeNil())

			// Build NOCSRElements TLV with vendor_reserved1 containing nodeID
			csrNonce := make([]byte, 32)
			rand.Read(csrNonce)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, []byte(testNodeID))

			// Generate attestation challenge
			attestationChallenge := make([]byte, 16)
			rand.Read(attestationChallenge)

			// Create WRONG signature (sign with a different key)
			wrongKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			wrongSignature, err := signAttestationData(nocsrElements, attestationChallenge, wrongKey)
			Expect(err).To(BeNil())

			// Verify attestation - should fail
			input := &MatterAttestationInput{
				NOCSRElements:        nocsrElements,
				AttestationChallenge: attestationChallenge,
				AttestationSignature: wrongSignature,
			}

			_, err = verifyMatterAttestation(ctx, input)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("attestation signature verification failed"))
		})

		It("should pass for pure Matter node (no vendor_reserved1)", func() {
			// Generate test CSR
			csrDER, _, err := generateTestCSR()
			Expect(err).To(BeNil())

			// Build NOCSRElements TLV WITHOUT vendor_reserved1
			csrNonce := make([]byte, 32)
			rand.Read(csrNonce)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, nil) // No vendor_reserved1

			// Generate attestation challenge
			attestationChallenge := make([]byte, 16)
			rand.Read(attestationChallenge)

			// Generate any signature (doesn't matter since verification is skipped)
			anyKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			anySignature, err := signAttestationData(nocsrElements, attestationChallenge, anyKey)
			Expect(err).To(BeNil())

			// Verify attestation - should pass without verification
			input := &MatterAttestationInput{
				NOCSRElements:        nocsrElements,
				AttestationChallenge: attestationChallenge,
				AttestationSignature: anySignature,
			}

			result, err := verifyMatterAttestation(ctx, input)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.CSR).To(Equal(csrDER))
			Expect(result.NodeID).To(BeEmpty()) // No nodeID extracted
		})

		It("should pass for node without registered certificates (pure Matter node)", func() {
			// Generate test CSR
			csrDER, _, err := generateTestCSR()
			Expect(err).To(BeNil())

			// Build NOCSRElements TLV with a nodeID that has NO registered certificates
			unregisteredNodeID := "UnregisteredPureMatterNode789"
			csrNonce := make([]byte, 32)
			rand.Read(csrNonce)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, []byte(unregisteredNodeID))

			// Generate attestation challenge
			attestationChallenge := make([]byte, 16)
			rand.Read(attestationChallenge)

			// Generate any signature (doesn't matter since node has no certs)
			anyKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			anySignature, err := signAttestationData(nocsrElements, attestationChallenge, anyKey)
			Expect(err).To(BeNil())

			// Verify attestation - should pass (treated as pure Matter node)
			input := &MatterAttestationInput{
				NOCSRElements:        nocsrElements,
				AttestationChallenge: attestationChallenge,
				AttestationSignature: anySignature,
			}

			result, err := verifyMatterAttestation(ctx, input)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.CSR).To(Equal(csrDER))
			Expect(result.NodeID).To(Equal(unregisteredNodeID))
		})

		It("should fail with invalid NOCSRElements TLV", func() {
			// Invalid TLV data
			invalidTLV := []byte{0x00, 0x01, 0x02, 0x03}

			input := &MatterAttestationInput{
				NOCSRElements:        invalidTLV,
				AttestationChallenge: make([]byte, 16),
				AttestationSignature: make([]byte, 64),
			}

			_, err := verifyMatterAttestation(ctx, input)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("failed to parse NOCSRElements"))
		})
	})

	Describe("handleVerify with Matter attestation", func() {
		var (
			matterGroupID string
		)

		BeforeEach(func() {
			// Create a Matter group
			opts := &group.CreateGroupOptions{
				Capabilities: []string{"matter"},
			}
			matterGroup, err := group.CreateGroupForUserWithOptions(rmng_context, "Test Matter Attestation Group", opts)
			Expect(err).To(BeNil())
			matterGroupID = matterGroup.GroupID
		})

		It("should return error when nocsr_elements provided without attestation_challenge", func() {
			// Store Matter assoc request
			storeInDynamoDBMatter(ctx, "test-attest-req-1", "challenge123", userID, matterGroupID)

			// Generate test CSR
			csrDER, _, _ := generateTestCSR()
			csrNonce := make([]byte, 32)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, nil)

			// Build request with nocsr_elements but missing attestation fields
			requestBody := map[string]string{
				"nocsr_elements": hex.EncodeToString(nocsrElements),
				// Missing attestation_challenge and attestation_signature
			}
			requestBodyJSON, _ := json.Marshal(requestBody)

			verifyRequest := events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"groupId":   matterGroupID,
					"requestId": "test-attest-req-1",
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				Body: string(requestBodyJSON),
			}

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("attestation_challenge and attestation_signature are required"))
		})

		It("should return error for invalid nocsr_elements hex encoding", func() {
			storeInDynamoDBMatter(ctx, "test-attest-req-2", "challenge123", userID, matterGroupID)

			// Build request with invalid hex
			requestBody := map[string]string{
				"nocsr_elements":        "ZZZZ", // Invalid hex
				"attestation_challenge": "0102030405060708090a0b0c0d0e0f10",
				"attestation_signature": "0102030405060708090a0b0c0d0e0f100102030405060708090a0b0c0d0e0f100102030405060708090a0b0c0d0e0f100102030405060708090a0b0c0d0e0f10",
			}
			requestBodyJSON, _ := json.Marshal(requestBody)

			verifyRequest := events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"groupId":   matterGroupID,
					"requestId": "test-attest-req-2",
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				Body: string(requestBodyJSON),
			}

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid nocsr_elements hex encoding"))
		})

		It("should return error when CSRNonce does not match challenge", func() {
			// Use a proper 64-char hex challenge (32 bytes)
			matterChallenge := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
			storeInDynamoDBMatter(ctx, "test-attest-req-nonce", matterChallenge, userID, matterGroupID)

			// Generate test CSR
			csrDER, _, err := generateTestCSR()
			Expect(err).To(BeNil())

			// Build NOCSRElements with WRONG CSRNonce (doesn't match challenge)
			wrongNonce := make([]byte, 32)
			rand.Read(wrongNonce) // Random nonce that won't match
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, wrongNonce, nil)

			attestationChallenge := make([]byte, 16)
			rand.Read(attestationChallenge)

			anyKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			attestationSignature, _ := signAttestationData(nocsrElements, attestationChallenge, anyKey)

			// Build request
			requestBody := map[string]string{
				"nocsr_elements":        hex.EncodeToString(nocsrElements),
				"attestation_challenge": hex.EncodeToString(attestationChallenge),
				"attestation_signature": hex.EncodeToString(attestationSignature),
			}
			requestBodyJSON, _ := json.Marshal(requestBody)

			verifyRequest := events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"groupId":   matterGroupID,
					"requestId": "test-attest-req-nonce",
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				Body: string(requestBodyJSON),
			}

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("CSRNonce does not match the challenge from initiate"))
		})

		It("should generate NOC successfully with attestation for pure Matter node", func() {
			// Use a proper 64-char hex challenge (32 bytes)
			matterChallenge := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
			storeInDynamoDBMatter(ctx, "test-attest-req-3", matterChallenge, userID, matterGroupID)

			// Generate test CSR
			csrDER, _, err := generateTestCSR()
			Expect(err).To(BeNil())

			// Build NOCSRElements TLV with CSRNonce matching the challenge
			csrNonce, _ := hex.DecodeString(matterChallenge)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, nil)

			attestationChallenge := make([]byte, 16)
			rand.Read(attestationChallenge)

			anyKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			attestationSignature, _ := signAttestationData(nocsrElements, attestationChallenge, anyKey)

			// Build request
			requestBody := map[string]string{
				"nocsr_elements":        hex.EncodeToString(nocsrElements),
				"attestation_challenge": hex.EncodeToString(attestationChallenge),
				"attestation_signature": hex.EncodeToString(attestationSignature),
			}
			requestBodyJSON, _ := json.Marshal(requestBody)

			verifyRequest := events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"groupId":   matterGroupID,
					"requestId": "test-attest-req-3",
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				Body: string(requestBodyJSON),
			}

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var responseBody map[string]string
			json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(responseBody["message"]).To(Equal("success"))
			Expect(responseBody["noc"]).To(ContainSubstring("-----BEGIN CERTIFICATE-----"))
			Expect(responseBody["matter_node_id"]).NotTo(BeEmpty())
		})

		It("should reject a second verify on the same request", func() {
			matterChallenge := "c1c2c3c4c5c6c7c8c9cacbcccdcecfd0d1d2d3d4d5d6d7d8d9dadbdcdddedfe0"
			storeInDynamoDBMatter(ctx, "test-attest-replay", matterChallenge, userID, matterGroupID)

			csrDER, _, err := generateTestCSR()
			Expect(err).To(BeNil())
			csrNonce, _ := hex.DecodeString(matterChallenge)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, nil)

			attestationChallenge := make([]byte, 16)
			rand.Read(attestationChallenge)
			anyKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			attestationSignature, _ := signAttestationData(nocsrElements, attestationChallenge, anyKey)

			verifyRequest := matterVerifyRequest(matterGroupID, "test-attest-replay", userID, map[string]string{
				"nocsr_elements":        hex.EncodeToString(nocsrElements),
				"attestation_challenge": hex.EncodeToString(attestationChallenge),
				"attestation_signature": hex.EncodeToString(attestationSignature),
			})

			response, err := handleVerify(ctx, verifyRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// The row survives until confirm, so replay protection has to come from the status check.
			response, err = handleVerify(ctx, verifyRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("expired or already used"))
		})

		It("should reject a request whose expiry has passed", func() {
			matterChallenge := "d1d2d3d4d5d6d7d8d9dadbdcdddedfe0e1e2e3e4e5e6e7e8e9eaebecedeeeff0"
			storeInDynamoDBMatter(ctx, "test-attest-expired", matterChallenge, userID, matterGroupID)
			Expect(expireAssocRequest(ctx, "test-attest-expired")).To(BeNil())

			csrDER, _, err := generateTestCSR()
			Expect(err).To(BeNil())
			csrNonce, _ := hex.DecodeString(matterChallenge)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, nil)

			attestationChallenge := make([]byte, 16)
			rand.Read(attestationChallenge)
			anyKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			attestationSignature, _ := signAttestationData(nocsrElements, attestationChallenge, anyKey)

			response, err := handleVerify(ctx, matterVerifyRequest(matterGroupID, "test-attest-expired", userID, map[string]string{
				"nocsr_elements":        hex.EncodeToString(nocsrElements),
				"attestation_challenge": hex.EncodeToString(attestationChallenge),
				"attestation_signature": hex.EncodeToString(attestationSignature),
			}))

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("expired or already used"))
		})

		It("should reject a CSR on a curve Matter forbids without panicking", func() {
			matterChallenge := "e1e2e3e4e5e6e7e8e9eaebecedeeeff0f1f2f3f4f5f6f7f8f9fafbfcfdfeff00"
			storeInDynamoDBMatter(ctx, "test-attest-p224", matterChallenge, userID, matterGroupID)

			// P-224 parses and self-verifies fine but crypto/ecdh rejects it, which used to nil-deref in the subject key ID derivation.
			csrDER, err := generateTestCSRWithCurve(elliptic.P224())
			Expect(err).To(BeNil())
			csrNonce, _ := hex.DecodeString(matterChallenge)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, nil)

			attestationChallenge := make([]byte, 16)
			rand.Read(attestationChallenge)
			anyKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			attestationSignature, _ := signAttestationData(nocsrElements, attestationChallenge, anyKey)

			response, err := handleVerify(ctx, matterVerifyRequest(matterGroupID, "test-attest-p224", userID, map[string]string{
				"nocsr_elements":        hex.EncodeToString(nocsrElements),
				"attestation_challenge": hex.EncodeToString(attestationChallenge),
				"attestation_signature": hex.EncodeToString(attestationSignature),
			}))

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to generate device NOC"))
		})

		It("should generate random node_id for pure Matter node via handleVerify", func() {
			// Use a proper 64-char hex challenge (32 bytes)
			matterChallenge := "b1b2b3b4b5b6b7b8b9babbbcbdbebfc0c1c2c3c4c5c6c7c8c9cacbcccdcecfd0"
			storeInDynamoDBMatter(ctx, "test-attest-req-4", matterChallenge, userID, matterGroupID)

			// Generate test CSR
			csrDER, _, err := generateTestCSR()
			Expect(err).To(BeNil())

			// Build NOCSRElements TLV with CSRNonce matching the challenge
			csrNonce, _ := hex.DecodeString(matterChallenge)
			nocsrElements := buildTestNOCSRElementsTLV(csrDER, csrNonce, nil)

			attestationChallenge := make([]byte, 16)
			rand.Read(attestationChallenge)

			anyKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			attestationSignature, _ := signAttestationData(nocsrElements, attestationChallenge, anyKey)

			// Build request and call handleVerify
			requestBody := map[string]string{
				"nocsr_elements":        hex.EncodeToString(nocsrElements),
				"attestation_challenge": hex.EncodeToString(attestationChallenge),
				"attestation_signature": hex.EncodeToString(attestationSignature),
				// NO node_id - should be auto-generated
			}
			requestBodyJSON, err := json.Marshal(requestBody)
			Expect(err).To(BeNil())

			verifyRequest := events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"groupId":   matterGroupID,
					"requestId": "test-attest-req-4",
				},
				Resource: "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				Body: string(requestBodyJSON),
			}

			response, err := handleVerify(ctx, verifyRequest)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var responseBody map[string]string
			json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(responseBody["message"]).To(Equal("success"))
			Expect(responseBody["noc"]).To(ContainSubstring("-----BEGIN CERTIFICATE-----"))
			Expect(responseBody["matter_node_id"]).NotTo(BeEmpty())

			// Verify that the generated node_id is 16 hex characters (8 bytes)
			// For pure Matter nodes, we generate a random 64-bit hex string as node_id
			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			getItemInput := &dynamodb.GetItemInput{
				TableName: aws.String(assoc_request_db.AssocRequestsTable),
				Key: map[string]types.AttributeValue{
					"request_id": &types.AttributeValueMemberS{Value: "test-attest-req-4"},
				},
			}
			result, err := dbMock.GetItem(ctx, getItemInput)
			Expect(err).To(BeNil())
			Expect(result.Item).NotTo(BeNil())

			// Check that a node_id was stored (auto-generated for pure Matter node)
			nodeIDAttr, ok := result.Item["node_id"]
			Expect(ok).To(BeTrue(), "node_id should be stored in assoc-request")
			storedNodeID := nodeIDAttr.(*types.AttributeValueMemberS).Value
			Expect(len(storedNodeID)).To(Equal(16), "Generated node_id should be 16 hex characters")
		})
	})
})

var _ = AfterSuite(func() {
	if profile != nil {
		fmt.Fprintf(timingFile, "\n--- User Node Association (Verify) ---\n")
		profile.Print(timingFile)
		fmt.Fprintf(timingFile, "-----------------------------\n\n")
	}
	timingFile.Close()
})
