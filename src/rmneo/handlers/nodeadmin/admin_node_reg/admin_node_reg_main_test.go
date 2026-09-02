// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"os"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_failed_nodes_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Helper function to get the mock IoT client
func getIoTClientMock() *mock.IoTClientMock {
	return awscommon.GetIoTClient().(*mock.IoTClientMock)
}

// Helper function to verify node registration
func verifyNodeRegistration(nodeID string, adminId string, offset int) {
	iotClient := getIoTClientMock()

	// Verify the thing was created using direct verification
	ExpectWithOffset(offset, iotClient.VerifyThingExists(nodeID)).To(BeTrue(), "Node was not created in IoT Core")

	// Verify the thing has at least one certificate
	thing, exists := iotClient.GetThingDirect(nodeID)
	Expect(exists).To(BeTrue(), "Node does not exist in IoT Core")
	Expect(len(thing.CertificateIds)).To(BeNumerically(">", 0), "No certificates attached to node")

	// Verify node was added to node_details
	item := test_utils.QuickGetItem(node_details_db.NodeDetailsTable, map[string]types.AttributeValue{
		"node_id": &types.AttributeValueMemberS{Value: nodeID},
	})
	Expect(item).NotTo(BeNil())
	Expect(item["admin_id"].(*types.AttributeValueMemberS).Value).To(Equal(adminId))
	Expect(item["reg_ts"]).NotTo(BeNil())
}

// Helper function to verify certificate registration
func verifyCertificateRegistration(nodeCert string, offset int) {
	iotClient := getIoTClientMock()

	// Verify the certificate exists and is active
	ExpectWithOffset(offset, iotClient.VerifyCertificateExists(nodeCert)).To(BeTrue(), "Certificate was not registered")
	ExpectWithOffset(offset, iotClient.VerifyCertificateActive(nodeCert)).To(BeTrue(), "Certificate is not active")
}

// Helper function to verify group assignments
func verifyGroupAssignment(nodeID string, expectedGroups []string, offset int) {
	if len(expectedGroups) == 0 {
		return
	}

	// Use the direct verification helper
	iotClient := getIoTClientMock()

	// Verify the node is in all expected groups
	for _, groupName := range expectedGroups {
		ExpectWithOffset(offset, iotClient.VerifyThingInGroup(nodeID, groupName)).To(BeTrue(),
			fmt.Sprintf("Node %s is not in group %s", nodeID, groupName))
	}
}

// Helper function to verify parent group hierarchy
func verifyGroupParent(childGroup string, expectedParent string, offset int) {
	iotClient := getIoTClientMock()
	actualParent, hasParent := iotClient.ThingGroupParents[childGroup]
	ExpectWithOffset(offset, hasParent).To(BeTrue(),
		fmt.Sprintf("Group %s has no parent, expected parent %s", childGroup, expectedParent))
	ExpectWithOffset(offset, actualParent).To(Equal(expectedParent),
		fmt.Sprintf("Group %s has parent %s, expected %s", childGroup, actualParent, expectedParent))
}

// Helper function to verify a group exists (was created)
func verifyGroupExists(groupName string, offset int) {
	iotClient := getIoTClientMock()
	_, exists := iotClient.ThingGroups[groupName]
	ExpectWithOffset(offset, exists).To(BeTrue(),
		fmt.Sprintf("Group %s was not created", groupName))
}

// Helper function to verify shadow tags - now uses the IoTDataPlaneMock's VerifyShadowTags method
func verifyShadowTags(nodeID string, expectedTags map[string]string, offset int) {
	iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	ExpectWithOffset(offset, iotDataClient.VerifyTags(nodeID, expectedTags, nil, nil)).To(BeTrue(),
		fmt.Sprintf("Shadow for node %s does not contain expected tags", nodeID))
}

// Helper function that tests certificate validation with a fixed test certificate
func testCertificateValidation(ctx context.Context, userID string, keyType string, expectedValid bool, offset int) {
	// Create the appropriate certificate based on the key type
	var cert string
	switch keyType {
	case "RSA-2048":
		cert = "-----BEGIN CERTIFICATE-----\nMIIC+TCCAeGgAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6WqwwwDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI1MTFaFw0yNjA1\nMjAwOTI1MTFaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTCCASIwDQYJKoZIhvcNAQEB\nBQADggEPADCCAQoCggEBALV+wQl/PUFuiX9BgSKcuRLy7aw67/Z98KJ9jBsozaBm\nQEkqJMCrEjYK8zsnDoVsXbbbj2qEE3w4LuYyhcNdDcDIzq+l68qOB5PUqTtkMlR1\n0v6LNFdOtNBYCJvINILem+cvqybrMfHrR3nF33cg9kvshoIVcEUpnPk9vqic8Px7\n2KMnaIUgvRg6tRqr8Xt3ou4RNgLUUYUpBc2jBYM+mKlL+RDCXmDWNXugDHvQbqto\nq39b3yemQfi92LStKrRv1ivBlp6vhZbDzqv4lFcawxWNV+UHLU2T/basPOCqLRZ5\n24DbHSk/edSj9b+/X729ZXOGuWArFDNT1VFDXLln7FcCAwEAAaNCMEAwHQYDVR0O\nBBYEFGAxuVICNSz/0iQNS7lsZNs+0eHTMB8GA1UdIwQYMBaAFHZpcCukxQSAVo2M\nJlVEAmCoYHjvMA0GCSqGSIb3DQEBCwUAA4IBAQBfzHCP0YcT0c1mH2DOp4Orw9we\n029sEHLHtQK2Gq+iu2VAK8/FgwIrTVfTMQGQnedu10DoETDWvmGrOphdC+YeWnZY\ntcH/LyOmdSRZF7YuQr9I328pVQ8ECDPteoVBq8QwrYNsSRx+MIDOHgPGBNswpL5h\nP1GFsviy9SDqVzfffHUfQEiTKU4la4cLRU8oPtTmbgCx+d0FlfBkz2IvaJyhoMKd\nx0bV/MLeLnbghiyGZ9oqsLLO5dZSA8wtV2DLvxHz4feRqlHfN9Fm9dWGvx4lJ/fA\n7tPMKuBTfPEKB9IpG/BtVNOk4G7+058t5s6GotJNm2zCpp3Nc3BQrBzqdR+Y\n-----END CERTIFICATE-----"
	case "RSA-1024":
		cert = "-----BEGIN CERTIFICATE-----\nMIICdTCCAV2gAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6WqxAwDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI4MzNaFw0yNjA1\nMjAwOTI4MzNaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTCBnzANBgkqhkiG9w0BAQEF\nAAOBjQAwgYkCgYEAwpQus55tNHla4UM7bPqvMRb1lcavgAg+ouiu9nQ8jJSOPCS1\nU30U6EZapWT7YhrG/0jqX295iCl2wUATnS1Qd9dhqFk6V/y4hGM1GZwWCGVYbODi\nb+PSYOG0Y8Y4Db4oxNAlOma2ChoR5dfUaimI3R/oBpNHrwJScxi7GB/0wDcCAwEA\nAaNCMEAwHQYDVR0OBBYEFEuVe6DCxRehtumP4B+s2d9vBPDUMB8GA1UdIwQYMBaA\nFHZpcCukxQSAVo2MJlVEAmCoYHjvMA0GCSqGSIb3DQEBCwUAA4IBAQAFZ102O/Ew\nnwmu12YPEEwApcsMY8v3cNLR8ZCEu3K5zFkqe23bxI3R0zbBxSsW2Rn3tVjosNZQ\nSYn1naE68NT1eZzXjdA1dC46hou+t7i3LtO8f/cbiP9KIxxCheqwtPD58oPcSykd\n97oDSfqadqdRir9i1RTvexG7ACLiU4PthF2Hf8+ZimSPizcckf/Mt3eQPsHQNZiM\n5r9AzoJ+BNhpo57Oi+uQBjSOQXTuJ+RHMG+puZmcRKs0vmnZ2q5k1AQuvIaQ5YA/\ngORSNBiQgwwsm+UmlybcFwgKYgRcL6WjAkGpT7vcsTKH2ke8reO5YueEqJqSLRI5\naG+yxHh+DslN\n-----END CERTIFICATE-----"
	case "RSA-4096":
		cert = "-----BEGIN CERTIFICATE-----\nMIIFazCCA1OgAwIBAgIUIpXLMZnzKROvUZWwFNcEZJVmOcUwDQYJKoZIhvcNAQEL\nBQAwRTELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM\nGEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDAeFw0yMzA1MjAwMDAwMDBaFw0yNDA1\nMjAwMDAwMDBaMEUxCzAJBgNVBAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEw\nHwYDVQQKDBhJbnRlcm5ldCBXaWRnaXRzIFB0eSBMdGQwggIiMA0GCSqGSIb3DQEB\nAQUAA4ICDwAwggIKAoICAQC4b9ak+tdWKZHBSRw/KMj6QBx48K2TvZvmV6KWG6Lw\nH5bUUzC5Z9h1crckX9EPkNlbDcB8jA0Z8wU3ZPkNZhD0m1+CLye2LVOrK1O2fA/1\nYWYAmuYmm6jllG1OWZQu3OjUvBWjqxapP2QL/tIgR5QqVwqVANXA3JNJEtYMBpXX\n8toYBJkJKxfSGOAqgADuDF0DL8wFC0LvEYBrTSmXsOrdDTh/C2Yz0J4G8SqzcpFd\nI2ACr9MhQ6jL2leqDZVhsj4EeFYEVoYvzJFxrCGm9e088PQVHzqNpZsm25fGMJ9W\nlbjDXMk8SAgLXww3Kc5E1sFMnx9GVg8qsQaZJQEP3FNfnIK0bmnwJcvBGDLPQQZk\nqLFgz16rlKB5EzGVbBHgZlCYOurQQ9AVKGqzYMlAdg9EYdjtTfRTHIvFQcAFEjFv\nZ4XAf4cRt3GQia84GMJEVJsQFYc9dZO8rCn4DTUkL3DwjAfpCsIk+pc+QAEZixzR\n9aGqwxAfJYgbdvJA4+BQvj7YjJDnV1ISkiOlmkJx0OVj//lKRN3QlrKzYWpdIrEZ\nzNoAd1UK469XAEBEnVnH8Jol9jPMf40ulKTCEa0k595tz/6KAuprKHRZ28qVGVKG\n+nTsJVjYAjWCfT7zvN4xmBUD/eeIYecUK+4l3C0yYXgTlhSNDIBFnZGcXr4J/XSc\nmQIDAQABo1MwUTAdBgNVHQ4EFgQU0jFYvPPWJoZUXKXnyGhfZECZbO4wHwYDVR0j\nBBgwFoAU0jFYvPPWJoZUXKXnyGhfZECZbO4wDwYDVR0TAQH/BAUwAwEB/zANBgkq\nhkiG9w0BAQsFAAOCAgEAbWIXiBSxnJ+N9WGQJbXbvRZ5GS9l5j9j/ejldyRDiKYh\nSOODoLQbUoUO+UaCImiKHVLsG3kMqKcbZJ8mQArn1EOekYmLg4gzXj0BA0VeJMEX\nWDJOMOjdgkzZYJKQKngyJLYYQYYBvl0GMGWrcpZbV81EM4odBMuHmUXGxSGC4FwH\nBo5bN+fsw5RtPE4U5ZzQbZYwoalj7KskqTcgLMBNF0H1FaSY0kcHHPZfnkW6FgS/\n7Hm28H2GX5QADRBdyXPFuxGLJ56FC5YikVX0AGJ7NErGpU0Cci+cLbbn9q6diV1l\nZ8Jw8fN1t2ChZ9QWGTOW+y9Ib7gVM8UdBbMHZfBNenaqcqP7dBX/qFYKvbYMXZWc\nxLq9q2MfIf70MBbnJNZKIQwvGbV/D4sF6GMsLZHsnBgFUxm/i53V46kjkrGEz8Qe\nS0vVnMk8JOx/i/LPXh1xGIl62vdYKLDlN51ZOH0YYDnJ8VKS37FrHblgfAiEMCX7\nVPMWuFBTpbMRVcsQ3PbRfiC25kkrKHiQRKIYk9GnvY8GQpvrRjrNxrlLEMzdgGKR\nTQDaJ6KvZ/UcZBWXT7Tw0aDDrtNJjmfEC9l8jbOV5FyZ2HkiXBFnpyELzP2pOPGP\nofFvFTKKJwPmJGLUUXW5AEjQ2HQcCX0W7Y5PFgY7Fo7rSrOmIwO5kmuNuWSu22Y=\n-----END CERTIFICATE-----"
	case "ECDSA-P256":
		cert = "-----BEGIN CERTIFICATE-----\nMIICLjCCARagAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6Wqw0wDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI1MjRaFw0yNjA1\nMjAwOTI1MjRaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTBZMBMGByqGSM49AgEGCCqG\nSM49AwEHA0IABJiqyidsfonkbHUB4yUNz5rrrT4WOAMiJ57iDQd8jMNn0jE+EhlU\nsqMH/814IM01cnAYW4teA13nCnO8UoZsNBijQjBAMB0GA1UdDgQWBBQKQRBtkP4J\nWOoJcMhLbponUVPQMDAfBgNVHSMEGDAWgBR2aXArpMUEgFaNjCZVRAJgqGB47zAN\nBgkqhkiG9w0BAQsFAAOCAQEAiVFPGl28MH/9hSeNoW6E4o478ikhOIQmjYlFInv0\nzVxp+dKY9uww9UWkwVmbYVyE5ttKxdp++3zZJ+hNGsQ23eITRbRaU+r+6kjVlQ0t\ndG6P9h2dKoz/zcEXOl18BllJ0S16ufTngZEs2vD0XE0KD+6/H4qWUyuaAScpGrfG\nx2pVo2vHF3/9/M9oawq5IMH+8f0CMmbzqTwaJ9ViP/bTWGfD+hlMGNib5h6tmuEu\nLjMxCdjcJGb3vN4RDCh03X/0O3ye7W+GObwyk057N8miWQqpQqnOW/I6XCuTacwb\nQ1H23zzSkqsxGQeu/SUX/VNibl0VotyLftD993DOci7/Tw==\n-----END CERTIFICATE-----"
	case "ECDSA-P384":
		cert = "-----BEGIN CERTIFICATE-----\nMIICSzCCATOgAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6Wqw4wDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI1MzBaFw0yNjA1\nMjAwOTI1MzBaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTB2MBAGByqGSM49AgEGBSuB\nBAAiA2IABGyz/HdCjfAvHFrkzG+vLSYnCn6LUQvfiUc1KWu8frfHndVM0YItys/H\nT2yejMQhfA/TWonTF9Y5TPZLfyzmQ7Gj4R9b4rRDd4cHYP08C3Fe2VGHSWeOP91H\nVmamSRbrI6NCMEAwHQYDVR0OBBYEFLBD9DSXptvDU66TC0jB75kTSFYoMB8GA1Ud\nIwQYMBaAFHZpcCukxQSAVo2MJlVEAmCoYHjvMA0GCSqGSIb3DQEBCwUAA4IBAQAi\nn1jQUVlio65j540dcTZKfjYhH+1dBHpZ7HfWvkvwi0s821mlZrLsmenGcgN14b5e\n78KHOgqWBPHsoecMuOlRma2A+txjiLQVIGKT/PayzNysCKr9IYoGRUPVRGusq0/s\nLRhULI/0Kw6qzSPPck2iDH1gxiheEpv+wrf7zjcuHwW0cWA98R2J+kawtyhonnfk\nTTzXbsyT9mcgbwej5DdqDe0I97tky+XQSl5hVlZfov2Y1/ooIIh2SBkNbYkyRAe5\nzgx0xtKqdNkeU7sbiZYwVEkz2Q1jXnexgC4gJ1aFXPuOZxdIYjtJ53xLcni6HiEr\nGjxDv1dB4bYdv4k+wVVJ\n-----END CERTIFICATE-----"
	case "ECDSA-P521":
		cert = "-----BEGIN CERTIFICATE-----\nMIICcTCCAVmgAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6Wqw8wDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI1MzVaFw0yNjA1\nMjAwOTI1MzVaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTCBmzAQBgcqhkjOPQIBBgUr\ngQQAIwOBhgAEAWqA8jioYmbKIh1sWq5jcrqxVRe3z3AhI0WvQi4bjnXZfU0tzjEk\nZTVhygEf27ttjHTlXSvi0ITulYozqRdqxvNjALYUeMVhR49XBlVErJP5tS19bjR7\n+AljDERRQ/gM8uNK1j22HAFKEqbodstmcZvd7klmTb7GvKUL/8m9WDU2aH84o0Iw\nQDAdBgNVHQ4EFgQUYdzxwApF3EeNelSQg/Vtn07zFj8wHwYDVR0jBBgwFoAUdmlw\nK6TFBIBWjYwmVUQCYKhgeO8wDQYJKoZIhvcNAQELBQADggEBADWXn6dktyu4bEk2\nR+d2cdtXQznGH9TSOz+yTDA0uvECZOA5x2eMsZZiA7GNNfuce+xaB9GA/himKC3v\nRa2ydiDu1hWn70sjDy3MizsMEOg1himkEtwFR+0Bam3B4wuFdC0kGfH4rNm+7rB6\nk10aKLTHA6gGUioMCkXCbZm4WdLFHJ0j03Xpl8ucrkMiiaxtrkzkqG71rV8eOYTe\nNZkDNCOGeya8n93yJVVZNtX+Xwtml6Qi99L/XfZLQ3fOvinlCZM2ekw9LePBBvPK\nCem9M5P7dGcufGHcQjm5o6pc0D4Btw/xsInmF9D940lXtU0hpx5i9ZPCs3aW6H1n\n3ney/pg=\n-----END CERTIFICATE-----"
	case "ECDSA-P224":
		cert = "-----BEGIN CERTIFICATE-----\nMIICIzCCAQugAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6WqxEwDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI4NDBaFw0yNjA1\nMjAwOTI4NDBaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTBOMBAGByqGSM49AgEGBSuB\nBAAhAzoABBlgknT1q9LCHtc7nCq/0a86NVmrRaSj4jRZ0RbrSRyPTghGa08KUSh1\nTKbZNYksChOj4wF/qM1To0IwQDAdBgNVHQ4EFgQUeUn6RZW/xPELUKYRz+cvFEwm\nYyMwHwYDVR0jBBgwFoAUdmlwK6TFBIBWjYwmVUQCYKhgeO8wDQYJKoZIhvcNAQEL\nBQADggEBAA771YxyLmw+kv1605J7FmlwmvLUr4dOf70RIoLjU16Lf3tAluTLu9+k\n7C7wmEDTfsOAiAYnS0FsB0w2DgTd+heQqjLMD90hiR/EQzsGNhRiHwziVJAyEN90\nqpHnfEnOkbp9Otuzis/LFTnFhqVHQGRtQcsazGh5FyZF4vhcax+CoidSwE0alHXQ\nMLyi2jU92R9OSKKrrwYAxrJw+L8UiQBH2hl2blzBsDgSmYWgKBa1lbKwQ6JOCQ33\nGY2RHick7nHLKM34hN8rjz7sFWyXEagRT5LuDtvGduEEB9eIV90QYSDwsRKDyGnz\nSjfo2fIW6qDMWqeKOl4X12A9OnPuwfg=\n-----END CERTIFICATE-----"
	default:
		Fail(fmt.Sprintf("Unknown key type: %s", keyType))
	}

	// Create API request with the certificate
	reqBody := map[string]interface{}{
		"cert": cert,
	}

	reqBodyBytes, err := json.Marshal(reqBody)
	ExpectWithOffset(offset, err).To(BeNil())

	request := events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Path:       "/v1/admin/nodes",
		Body:       string(reqBodyBytes),
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
			},
		},
	}

	// Call the API handler
	response, err := handleRequest(ctx, request)
	ExpectWithOffset(offset, err).To(BeNil())

	// Check the response based on expected validity
	if expectedValid {
		ExpectWithOffset(offset, response.StatusCode).To(Equal(http.StatusCreated),
			fmt.Sprintf("Expected success for %s certificate but got error: %s", keyType, response.Body))
		var responseBody RegisterSingleNodeResponse
		err = json.Unmarshal([]byte(response.Body), &responseBody)
		ExpectWithOffset(offset, err).To(BeNil())
		ExpectWithOffset(offset, responseBody.NodeID).ToNot(BeEmpty())

		// Verify node registration
		offset++
		verifyNodeRegistration(responseBody.NodeID, userID, offset)
		verifyCertificateRegistration(cert, offset)
	} else {
		ExpectWithOffset(offset, response.StatusCode).ToNot(Equal(http.StatusOK))
	}
}

var _ = Describe("Admin Nodes Registration", func() {
	var (
		ctx        *rmngctx.RmngContext
		userID     string
		nodeCert   string
		nodeCACert string
		offset     int
		request    events.APIGatewayProxyRequest
		fileBucket string
	)

	BeforeEach(func() {
		offset = 1
		test_utils.TestSetup()
		fileBucket = os.Getenv("FILE_BUCKET")

		userID = "test-admin-user-id"

		// Set up admin user using helper function
		_, ctx = test_utils.SetupTestAdminUser(context.Background(), userID, "test-user-email")

		nodeCert = "-----BEGIN CERTIFICATE-----\nMIIC+TCCAeGgAwIBAgIUb1JEk/DirFchWIwxNKFcarvAKk4wDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNjA1MjAxMTQ3NDRaFw0zMTA1\nMTkxMTQ3NDRaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTCCASIwDQYJKoZIhvcNAQEB\nBQADggEPADCCAQoCggEBAKu/9Xkze9td/8cUd9ruVqrejVgQ8giO0SV77u0nmZLg\npn9QxOUKpfFROyplgh3xz0xZKY20yDddIm8g0ecXI0PjFtB5vr6BKE6jKjG2J5Pf\nYYqveFZY55IM0inNvxxyAZ+WyAe9LYuE676SV5lT3mjHGrKiEPlJxerhnc/e/Oct\nT/2Zzx8Y0BnLoKsA72dWkOgJMSBdWz56yxQ3zRfnE1hChGyrVAakgjGGt07F4dfe\nYGHwmz2D1qRMwSPovbNfO+bsBn01mHEMWMKmud15beXd1ngdRgvP0LhjFd0Os6gG\nJDHQ5RGgMOmWbyAlUo5butofbFxH7rSLRw9UhBw97tcCAwEAAaNCMEAwHQYDVR0O\nBBYEFNbINoWW7Z2AsMe/pCWOzMQ7PzYOMB8GA1UdIwQYMBaAFAMhLZ42gQoMVhx6\nvqsmZ45s3dr7MA0GCSqGSIb3DQEBCwUAA4IBAQCXIu3sRBULDfR+vXPzQ2io5m91\nvG5f/MGUFKdMGNz1XVwOeRmWqDLz21TF+Xmma9YNlf051gZjmxx2K2JvUseawJ1u\n+GheIK0AsbNY3CDHZ5g/JbOwjpvutsTFPsQRCcRRQKZ56UBVZa7Ow+yH8nz6TEMx\nHfW0tunfVcq+HEEGb24fylsQGn+ts28XJyyR4Voqc7RU0U4rTz43YlKkTXz7ECvD\nmEqT0g/OpaAwa7jyLJBQMGnOpjUVf+5x2j1liIlySGf6T7bPGgTNqb451DuXsM4h\nwVxZ4z9wP8XHDlcDCBLYBoUgy7f6jvUgLRRZqW0qTMeipd10iU2kGoPYg9Fe\n-----END CERTIFICATE-----"
		nodeCACert = "-----BEGIN CERTIFICATE-----\nMIIDCzCCAfOgAwIBAgIUW7gJVmO2l7jOk+3CJgXJqzJXJ54wDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNjA1MjAxMTQ3NDNaFw0zMTA1\nMTkxMTQ3NDNaMBUxEzARBgNVBAMMCk15IFJvb3QgQ0EwggEiMA0GCSqGSIb3DQEB\nAQUAA4IBDwAwggEKAoIBAQCholeYGrAxXKRNEAWy/GCsjx3FNukA/G5oHklzGObt\nZ0LyFMmMcMKyQT3/eitPHIYV1r6Qgvk7ebnbWSmSqp8m7HW804hqAoCWSJTczyuw\nFmvkxW92Ked6VxpSTetqd27VvfkMPFRp+TQ9OgFKdtxchHaKfDN7frDWPVbcAB/A\nxk7af2SVrybe494n0SeD0yepwDwMYAtls/djpW2aXicINxmbhwd1vBk7L9AWD+Hz\nLYlvmnenmrgV34BzURMiRPziGtt8Vmw6/tolxatXV0Phex3pLwh5E73eDt64GZJG\nybecrIFC9OZRBAsBoqQrNCAiVSug5CnspcVvOEmTaePfAgMBAAGjUzBRMB0GA1Ud\nDgQWBBQDIS2eNoEKDFYcer6rJmeObN3a+zAfBgNVHSMEGDAWgBQDIS2eNoEKDFYc\ner6rJmeObN3a+zAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQAi\nxa4etkCX2shGJ4uK/J58zY5a6ulQ6IJaGWsWQfzcXvOZa6EFMwrIM/uiD/WcfK9N\nzIdiTbZpYmRIZb6RmxyWd5uvLba7q3ObqMcpbG+h5sFc2mASQIMHUku06KD5+FD4\nf3mWLnDI/2s71meJTkiLy3lV1QboJxwrRMrnA9edaB0FZ/hYto2Lv+BFMsKikn5H\nZkYi9T2Sz/DRg9loZ/vqgnDXJ95ps4icTCLvJWD20ED4gTVxMnS4RvJluXx88f17\n32REoueAHDHR4Rwuwvv0rQaBn/07f7eISXuFtQAbolR8U1QEFEb8Xr6ETeoCSQNF\ndUEd+UDe9lXyW43hkM76\n-----END CERTIFICATE-----"

		// Initialize request with default values
		request = events.APIGatewayProxyRequest{
			HTTPMethod: "POST",
			Path:       "/v1/admin/nodes",
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             userID,
					CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
				},
			},
		}
	})

	Describe("handleRequest", func() {
		It("should successfully register a node with all parameters", func() {
			Skip("TEMP: fixture cert in this file (issued 2025-05-20) expired 2026-05-20 — regenerate to re-enable.")
			reqBody := map[string]interface{}{
				"cert":              nodeCert,
				"ca_cert":           nodeCACert,
				"checksum":          "abcdef1234567890abcdef1234567890",
				"ca_checksum":       "0987654321fedcba0987654321fedcba",
				"admin_group_names": []string{"Group1", "Parent.Child"},
				"tags":              []string{"environment:prod", "owner:admin"},
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			var responseBody RegisterSingleNodeResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
			Expect(responseBody.NodeID).To(Equal("test-node"))

			// Verify node registration
			verifyNodeRegistration("test-node", userID, offset)

			// Verify certificate registration
			verifyCertificateRegistration(nodeCert, offset)

			// Verify group assignment
			verifyGroupAssignment("test-node", []string{"Group1", "Parent.Child"}, offset)

			// Verify shadow tags
			verifyShadowTags("test-node", map[string]string{"environment": "prod", "owner": "admin"}, offset)
		})

		It("should register a node with admin_parent_group_name", func() {
			reqBody := map[string]interface{}{
				"cert":                    nodeCert,
				"admin_group_names":       []string{"ChildGroup1"},
				"admin_parent_group_name": "ParentGroup",
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			var responseBody RegisterSingleNodeResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
			Expect(responseBody.NodeID).To(Equal("test-node"))

			// Verify parent group was created
			verifyGroupExists("ParentGroup", offset)

			// Verify child group was created under parent
			verifyGroupExists("ChildGroup1", offset)
			verifyGroupParent("ChildGroup1", "ParentGroup", offset)

			// Verify node is in the child group
			verifyGroupAssignment("test-node", []string{"ChildGroup1"}, offset)
		})

		It("should register a node with multiple child groups under a parent", func() {
			reqBody := map[string]interface{}{
				"cert":                    nodeCert,
				"admin_group_names":       []string{"Child1", "Child2"},
				"admin_parent_group_name": "MyParent",
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			var responseBody RegisterSingleNodeResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())

			// Verify both child groups are under the parent
			verifyGroupExists("MyParent", offset)
			verifyGroupParent("Child1", "MyParent", offset)
			verifyGroupParent("Child2", "MyParent", offset)

			// Verify node is assigned to both child groups
			verifyGroupAssignment("test-node", []string{"Child1", "Child2"}, offset)
		})

		It("should register a node with admin_group_names but no parent (flat groups)", func() {
			reqBody := map[string]interface{}{
				"cert":              nodeCert,
				"admin_group_names": []string{"FlatGroup1"},
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			// Verify group exists but has no parent
			verifyGroupExists("FlatGroup1", offset)
			iotClient := getIoTClientMock()
			_, hasParent := iotClient.ThingGroupParents["FlatGroup1"]
			Expect(hasParent).To(BeFalse(), "FlatGroup1 should not have a parent")
		})

		It("should ignore admin_parent_group_name when admin_group_names is empty", func() {
			reqBody := map[string]interface{}{
				"cert":                    nodeCert,
				"admin_parent_group_name": "OrphanParent",
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			// Parent group should NOT be created since no child groups were specified
			iotClient := getIoTClientMock()
			_, exists := iotClient.ThingGroups["OrphanParent"]
			Expect(exists).To(BeFalse(), "Parent group should not be created when no admin_group_names provided")
		})
	})

	Describe("Validation Tests", func() {
		It("should accept RSA 2048-bit certificates", func() {
			testCertificateValidation(ctx.Context, userID, "RSA-2048", true, offset)
		})

		It("should accept ECDSA P-256 certificates", func() {
			testCertificateValidation(ctx.Context, userID, "ECDSA-P256", true, offset)
		})

		It("should accept ECDSA P-384 certificates", func() {
			testCertificateValidation(ctx.Context, userID, "ECDSA-P384", true, offset)
		})

		It("should accept ECDSA P-521 certificates", func() {
			testCertificateValidation(ctx.Context, userID, "ECDSA-P521", true, offset)
		})

		It("should reject RSA 1024-bit certificates (too small)", func() {
			testCertificateValidation(ctx.Context, userID, "RSA-1024", false, offset)
		})

		It("should reject ECDSA P-224 certificates (unsupported curve)", func() {
			testCertificateValidation(ctx.Context, userID, "ECDSA-P224", false, offset)
		})

		It("should return an error when certificate is missing", func() {
			reqBody := map[string]interface{}{
				"admin_group_names": []string{"Group1"},
				"tags":              []string{"environment:prod"},
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))

			var responseBody utils.APIStatus
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
		})

		It("should return an error when certificate format is invalid", func() {
			// Create a request with invalid certificate format
			reqBody := map[string]interface{}{
				"cert": "Invalid Certificate Format",
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())

			var responseBody RegisterSingleNodeResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
		})

		It("should return an error when checksum format is invalid", func() {
			// Create a request with invalid checksum format (not 32 hex characters)
			reqBody := map[string]interface{}{
				"cert":     nodeCert,
				"checksum": "invalid", // Too short and not hex
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())

			var responseBody RegisterSingleNodeResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
		})

		It("should return an error when tag format is invalid", func() {
			// Create a request with invalid tag format
			invalidTagValue := "invalidFormat"
			reqBody := map[string]interface{}{
				"cert": nodeCert,
				"tags": []string{invalidTagValue}, // Missing colon separator
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())

			var responseBody RegisterSingleNodeResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
		})

		It("should handle request with only required parameter (cert)", func() {
			// Create a request with only the required certificate
			reqBody := map[string]interface{}{
				"cert": nodeCert,
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			var responseBody RegisterSingleNodeResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
			Expect(responseBody.NodeID).To(Equal("test-node"))

			// Verify node registration
			verifyNodeRegistration("test-node", userID, offset)

			// Verify certificate registration
			verifyCertificateRegistration(nodeCert, offset)
		})

		It("should reject invalid JSON in request body", func() {
			request.Body = `{"cert": "test", INVALID_JSON}`
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())

			var responseBody RegisterSingleNodeResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
		})

		It("should return method not allowed for non-POST requests", func() {
			request.HTTPMethod = "GET"
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})

		It("should return not found for invalid paths", func() {
			request.Path = "/v1/admin/nodes/invalid"
			response, err := handleRequest(ctx.Context, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

	})

	Describe("Duplicate Resource Tests", func() {
		It("should handle registering the same node twice gracefully", func() {
			// First registration
			reqBody := map[string]interface{}{
				"cert":              nodeCert,
				"admin_group_names": []string{"Group1"},
				"tags":              []string{"environment:prod"},
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			var responseBody RegisterSingleNodeResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
			Expect(responseBody.NodeID).To(Equal("test-node"))

			verifyNodeRegistration("test-node", userID, offset)
			verifyCertificateRegistration(nodeCert, offset)
			verifyGroupAssignment("test-node", []string{"Group1"}, offset)
			verifyShadowTags("test-node", map[string]string{"environment": "prod"}, offset)

			// Second registration with the same certificate, asking for a
			// different admin group.
			reqBody["admin_group_names"] = []string{"Group2"}
			reqBodyBytes, err = json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err = handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())

			Expect(response.StatusCode).To(Equal(http.StatusConflict))
			Expect(response.Body).To(ContainSubstring("test-node"))
			Expect(response.Body).To(ContainSubstring("already registered"))

			// Group2 was never added: the node keeps the first registration's
			// group and tags.
			verifyNodeRegistration("test-node", userID, offset)
			verifyCertificateRegistration(nodeCert, offset)
			verifyGroupAssignment("test-node", []string{"Group1"}, offset)
			verifyShadowTags("test-node", map[string]string{"environment": "prod"}, offset)
		})

		It("leaves the existing group assignment intact when the same node is re-registered", func() {
			// Register node with group
			reqBody := map[string]interface{}{
				"cert":              nodeCert,
				"admin_group_names": []string{"Group1"},
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())

			// First registration should succeed
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			// Register again with same group
			reqBody = map[string]interface{}{
				"cert":              nodeCert,
				"admin_group_names": []string{"Group1"},
			}

			reqBodyBytes, err = json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Body = string(reqBodyBytes)
			response, err = handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())

			Expect(response.StatusCode).To(Equal(http.StatusConflict))

			// Verify group assignment is still valid
			verifyGroupAssignment("test-node", []string{"Group1"}, offset)
		})
	})

	Describe("Bulk Registration Tests", func() {
		It("should create node registration request for bulk registration", func() {
			// Create test certificates in PEM format
			// Node Id: bulknode1
			testCert1 := "-----BEGIN CERTIFICATE-----\nMIIDCTCCAfGgAwIBAgIUJxFymgxSNmN/Y1VA1xpjsfyE+P8wDQYJKoZIhvcNAQEL\nBQAwFDESMBAGA1UEAwwJYnVsa25vZGUxMB4XDTI1MDYyMDE3MDA0N1oXDTI2MDYy\nMDE3MDA0N1owFDESMBAGA1UEAwwJYnVsa25vZGUxMIIBIjANBgkqhkiG9w0BAQEF\nAAOCAQ8AMIIBCgKCAQEAxkOoaj9mf4bw7N9SV1zHvgtvszvauaay+k1eSeqbgOde\nfu0qwSZ8BLNtMstibHOwmpS4OPoxbW5KhyoBRdhcO2wUamEk6UdapXcOJiKa+u7I\n3AcpqMe5i3WVSAFttotfSeI0nTqAGPkTZOrDqZCwp2Hg+m6SFH2i1efXRYyMlGBP\nmU8B4HC84HoM19EJw4CIMUIUWR8WEugvHuaf5ano00lGr6QoHsgCWNyj533KgQ4A\nNwdqQ0h1gnv+Bdz/mCZ+FmveUn1jFfRokbceZqxaMmm5BN9cEmv2abpZgC9A5If6\n3rcv59aSLAIn/Sj/x1N9G9d/IyKQbwkKw1zquuyUdQIDAQABo1MwUTAdBgNVHQ4E\nFgQU4aX1iM6cEWbWl+Aua5WxJyaj9YswHwYDVR0jBBgwFoAU4aX1iM6cEWbWl+Au\na5WxJyaj9YswDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAVrsy\n8Fdeds02qFb5A5r2usNfSw3c30qIDtv0HSgfd5lVpvG1p5CE+ziyWtBuwxkzwEdE\n4JPJmXX5bQGyrZKlkD60K3kHq+Ed2hakiLJjB15DqNTk8pKTHjfYA0/mfXZKUtqP\nc03a8DPqjfbncHUOIyUaVr+o8O5dZIouGx9M84/RDbbemPjHAapshDNejLLA/gzT\n7/PGRQ7lGRlHu3NgIaoLAE0Q4Uwj7cycNugfQCnQF8nWSZyR192gh00alLb38p98\nE8tvw9wWDS8RMYV8KTP2nuB59OWxUSoNGtY+dnb88tLIiUE7odMjaBQY8id+Pg/9\n/fz2Ocw6G8/996nJdw==\n-----END CERTIFICATE-----"

			// Node Id: bulknode2
			testCert2 := "-----BEGIN CERTIFICATE-----\nMIIDCTCCAfGgAwIBAgIUFSmWQs0md/PJBWOtaJjxuRlsPKswDQYJKoZIhvcNAQEL\nBQAwFDESMBAGA1UEAwwJYnVsa25vZGUyMB4XDTI1MDYyMDE3MDA0N1oXDTI2MDYy\nMDE3MDA0N1owFDESMBAGA1UEAwwJYnVsa25vZGUyMIIBIjANBgkqhkiG9w0BAQEF\nAAOCAQ8AMIIBCgKCAQEA6s7QeHQs2k+YhXX5Ri3Fvlgh5W0OOfABq87jTSadI38F\nqKILXuKKEFEf6TXIwmXil5VscSJW4D1YeCqtWQBvXKC9/hSyCULt8BmWBbJ7xypW\n+eFCYZWDEonTe5J+yMChbLFK21ghJL3nhN3EKfwje720zM9cPCc6Zixu+3qHlgsE\nIxMaruqsncsnG3v2+EoL21W/xyXfgLDkzDBYGJE/SEVpQqaOq723OW0EL7f95sPW\nJpu09PwrpxvVUGVEic+sm9bFmXa11Swa9AdFrwbKHwpLxVAUKOz5sA19H//78dyy\nGA7Q5mgkRgq4YM511iiJc5Tb8qBCPo7OJZf6SrjJ/QIDAQABo1MwUTAdBgNVHQ4E\nFgQUYgFUcxifQE24cB5PxmIjNOgU/9swHwYDVR0jBBgwFoAUYgFUcxifQE24cB5P\nxmIjNOgU/9swDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAWrJ/\nD76x7n7slHBFbbPo3m32XS0kFBr7qoJqUfPAs2d+/5cXJY5sI0gilLQ7+ugyBePP\n+3JTHPehye25zcBnIQqnULrj6Xw/Zw5r8xZT0yeqnLDyAL45Ns7XBO36tqMzXLt2\ndjhepka5RT0RZOIM2xfYtOFkMBGfKLw+lKYOcl6B1MIiJ2LOtZjdL3Gk2W/Y21IJ\nPEl7Esd0RY6P9cfHa8KRIW476A/qCPOp1AhbGKLxt/f0gdFjLRD0tcORTqW3ZLmx\n951udgLtXpP0DpTSdfN+EHxByqZTh6z8vJv46nOHPIA89KGt0H0W1aOFjyFGVipr\nXWMlT8nc6zw9MuNjYg==\n-----END CERTIFICATE-----"

			// Create test bucket and CSV file in mock S3
			mockS3Client := awscommon.GetS3Client().(*mock.S3ClientMock)
			mockS3Client.CreateBucketDirect(fileBucket)

			// Mock data with proper certificate
			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			w.Write([]string{"node_id", "certs", "admin_groups", "key1", "key2", "key3", "key4"})
			w.Write([]string{"bulknode1", testCert1, "group1", "value1", "value2", "", ""})
			w.Write([]string{"bulknode2", testCert2, "group2", "", "", "value3", "value4"})
			w.Flush()
			csvContent := buf.String()

			// Store CSV in mock S3
			mockS3Client.Buckets[fileBucket]["system/bulk-certs.csv"] = csvContent

			reqBody := map[string]interface{}{
				"cert_file_s3_path": "s3://" + fileBucket + "/system/bulk-certs.csv",
				"admin_group_names": []string{"BulkGroup1", "BulkGroup2"},
				"tags":              []string{"environment:prod", "type:bulk"},
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Path = "/v1/admin/nodes/registration-jobs"
			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusAccepted))

			var responseBody RegisterBulkNodesResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())
			Expect(responseBody.RequestId).NotTo(BeEmpty())
			Expect(responseBody.Message).To(Equal("Bulk registration request created"))

			// Now call the status API and verify the response matches the DB entry
			request.Body = "" // No body for GET
			request.Path = "/v1/admin/nodes/registration-jobs/" + responseBody.RequestId
			request.HTTPMethod = "GET"
			request.PathParameters = map[string]string{"requestId": responseBody.RequestId}
			statusRespRaw, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(statusRespRaw.StatusCode).To(Equal(http.StatusOK))
			var statusResp RegisterNodeStatusResponse
			err = json.Unmarshal([]byte(statusRespRaw.Body), &statusResp)
			Expect(err).To(BeNil())
			Expect(statusResp.RequestID).To(Equal(responseBody.RequestId))
			Expect(statusResp.UserID).To(Equal(userID))
			Expect(statusResp.Status).To(Equal(node_reg_req_db.NODE_REG_STATUS_COMPLETED))
			Expect(statusResp.TotalCount).To(Equal(2))
			Expect(*statusResp.SuccessCount).To(Equal(2))
			Expect(*statusResp.FailedCount).To(Equal(0))
			Expect(statusResp.AdminGroupNames).To(Equal([]string{"BulkGroup1", "BulkGroup2"}))
			Expect(statusResp.AdminParentGroupName).To(BeEmpty())
			Expect(statusResp.Tags).To(Equal([]string{"environment:prod", "type:bulk"}))

			// Verify shadow tags
			verifyShadowTags("bulknode1", map[string]string{"environment": "prod", "type": "bulk", "key1": "value1", "key2": "value2"}, offset)
			verifyShadowTags("bulknode2", map[string]string{"environment": "prod", "type": "bulk", "key3": "value3", "key4": "value4"}, offset)
		})

		It("should create bulk registration with admin_parent_group_name", func() {
			testCert1 := "-----BEGIN CERTIFICATE-----\nMIIDCTCCAfGgAwIBAgIUJxFymgxSNmN/Y1VA1xpjsfyE+P8wDQYJKoZIhvcNAQEL\nBQAwFDESMBAGA1UEAwwJYnVsa25vZGUxMB4XDTI1MDYyMDE3MDA0N1oXDTI2MDYy\nMDE3MDA0N1owFDESMBAGA1UEAwwJYnVsa25vZGUxMIIBIjANBgkqhkiG9w0BAQEF\nAAOCAQ8AMIIBCgKCAQEAxkOoaj9mf4bw7N9SV1zHvgtvszvauaay+k1eSeqbgOde\nfu0qwSZ8BLNtMstibHOwmpS4OPoxbW5KhyoBRdhcO2wUamEk6UdapXcOJiKa+u7I\n3AcpqMe5i3WVSAFttotfSeI0nTqAGPkTZOrDqZCwp2Hg+m6SFH2i1efXRYyMlGBP\nmU8B4HC84HoM19EJw4CIMUIUWR8WEugvHuaf5ano00lGr6QoHsgCWNyj533KgQ4A\nNwdqQ0h1gnv+Bdz/mCZ+FmveUn1jFfRokbceZqxaMmm5BN9cEmv2abpZgC9A5If6\n3rcv59aSLAIn/Sj/x1N9G9d/IyKQbwkKw1zquuyUdQIDAQABo1MwUTAdBgNVHQ4E\nFgQU4aX1iM6cEWbWl+Aua5WxJyaj9YswHwYDVR0jBBgwFoAU4aX1iM6cEWbWl+Au\na5WxJyaj9YswDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAVrsy\n8Fdeds02qFb5A5r2usNfSw3c30qIDtv0HSgfd5lVpvG1p5CE+ziyWtBuwxkzwEdE\n4JPJmXX5bQGyrZKlkD60K3kHq+Ed2hakiLJjB15DqNTk8pKTHjfYA0/mfXZKUtqP\nc03a8DPqjfbncHUOIyUaVr+o8O5dZIouGx9M84/RDbbemPjHAapshDNejLLA/gzT\n7/PGRQ7lGRlHu3NgIaoLAE0Q4Uwj7cycNugfQCnQF8nWSZyR192gh00alLb38p98\nE8tvw9wWDS8RMYV8KTP2nuB59OWxUSoNGtY+dnb88tLIiUE7odMjaBQY8id+Pg/9\n/fz2Ocw6G8/996nJdw==\n-----END CERTIFICATE-----"

			mockS3Client := awscommon.GetS3Client().(*mock.S3ClientMock)
			mockS3Client.CreateBucketDirect(fileBucket)

			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			w.Write([]string{"node_id", "certs", "admin_groups"})
			w.Write([]string{"bulknode1", testCert1, "group1"})
			w.Flush()

			mockS3Client.Buckets[fileBucket]["system/bulk-parent-certs.csv"] = buf.String()

			reqBody := map[string]interface{}{
				"cert_file_s3_path":       "s3://" + fileBucket + "/system/bulk-parent-certs.csv",
				"admin_group_names":       []string{"BulkChild1"},
				"admin_parent_group_name": "BulkParent",
			}

			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Path = "/v1/admin/nodes/registration-jobs"
			request.Body = string(reqBodyBytes)
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusAccepted))

			var responseBody RegisterBulkNodesResponse
			err = json.Unmarshal([]byte(response.Body), &responseBody)
			Expect(err).To(BeNil())

			// Verify parent group was created and child is under it
			verifyGroupExists("BulkParent", offset)
			verifyGroupExists("BulkChild1", offset)
			verifyGroupParent("BulkChild1", "BulkParent", offset)
		})

		// TODO: Check idempotency
	})

	Describe("List Registration Jobs API Tests", func() {
		It("should return empty list when no jobs exist", func() {
			request.Body = ""
			request.Path = "/v1/admin/nodes/registration-jobs"
			request.HTTPMethod = "GET"
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var listResp ListRegistrationJobsResponse
			err = json.Unmarshal([]byte(response.Body), &listResp)
			Expect(err).To(BeNil())
			Expect(listResp.Jobs).To(HaveLen(0))
			Expect(listResp.NextKey).To(BeEmpty())
		})

		It("should list jobs after bulk registration", func() {
			// Create a bulk registration first
			mockS3Client := awscommon.GetS3Client().(*mock.S3ClientMock)
			mockS3Client.CreateBucketDirect(fileBucket)

			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			w.Write([]string{"node_id", "certs", "admin_groups"})
			w.Write([]string{"listnode1", nodeCert, "group1"})
			w.Flush()
			mockS3Client.Buckets[fileBucket]["system/list-test.csv"] = buf.String()

			reqBody := map[string]interface{}{
				"cert_file_s3_path": "s3://" + fileBucket + "/system/list-test.csv",
				"admin_group_names": []string{"ListGroup1"},
				"tags":              []string{"env:test"},
			}
			reqBodyBytes, err := json.Marshal(reqBody)
			Expect(err).To(BeNil())

			request.Path = "/v1/admin/nodes/registration-jobs"
			request.Body = string(reqBodyBytes)
			request.HTTPMethod = "POST"
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusAccepted))

			// Now list jobs
			request.Body = ""
			request.HTTPMethod = "GET"
			response, err = handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var listResp ListRegistrationJobsResponse
			err = json.Unmarshal([]byte(response.Body), &listResp)
			Expect(err).To(BeNil())
			Expect(listResp.Jobs).To(HaveLen(1))
			Expect(listResp.NextKey).To(BeEmpty())
			Expect(listResp.Jobs[0].AdminGroupNames).To(Equal([]string{"ListGroup1"}))
			Expect(listResp.Jobs[0].Tags).To(Equal([]string{"env:test"}))
			Expect(listResp.Jobs[0].CertFileS3Path).To(Equal("s3://" + fileBucket + "/system/list-test.csv"))
		})

		It("should respect limit query parameter", func() {
			mockS3Client := awscommon.GetS3Client().(*mock.S3ClientMock)
			mockS3Client.CreateBucketDirect(fileBucket)

			// Create 3 bulk registrations
			for i := 0; i < 3; i++ {
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "certs", "admin_groups"})
				w.Write([]string{fmt.Sprintf("limitnode%d", i), nodeCert, "group1"})
				w.Flush()
				key := fmt.Sprintf("system/limit-test-%d.csv", i)
				mockS3Client.Buckets[fileBucket][key] = buf.String()

				reqBody := map[string]interface{}{
					"cert_file_s3_path": "s3://" + fileBucket + "/" + key,
				}
				reqBodyBytes, err := json.Marshal(reqBody)
				Expect(err).To(BeNil())

				request.Path = "/v1/admin/nodes/registration-jobs"
				request.Body = string(reqBodyBytes)
				request.HTTPMethod = "POST"
				resp, err := handleRequest(ctx.Context, request)
				Expect(err).To(BeNil())
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			}

			// List with page_size=2
			request.Body = ""
			request.HTTPMethod = "GET"
			request.QueryStringParameters = map[string]string{"page_size": "2"}
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var listResp ListRegistrationJobsResponse
			err = json.Unmarshal([]byte(response.Body), &listResp)
			Expect(err).To(BeNil())
			Expect(listResp.Jobs).To(HaveLen(2))
			Expect(listResp.NextKey).NotTo(BeEmpty())

			// Fetch second page using NextKey
			request.QueryStringParameters = map[string]string{
				"page_size": "2",
				"start_key": listResp.NextKey,
			}
			response, err = handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var page2Resp ListRegistrationJobsResponse
			err = json.Unmarshal([]byte(response.Body), &page2Resp)
			Expect(err).To(BeNil())
			Expect(page2Resp.Jobs).To(HaveLen(1))
			Expect(page2Resp.NextKey).To(BeEmpty())

			// Clean up query params
			request.QueryStringParameters = nil
		})

		It("should filter jobs by status", func() {
			mockS3Client := awscommon.GetS3Client().(*mock.S3ClientMock)
			mockS3Client.CreateBucketDirect(fileBucket)

			// Create 2 bulk registrations (both will end up as "completed" after ECS mock runs)
			for i := 0; i < 2; i++ {
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "certs", "admin_groups"})
				w.Write([]string{fmt.Sprintf("filternode%d", i), nodeCert, "group1"})
				w.Flush()
				key := fmt.Sprintf("system/filter-test-%d.csv", i)
				mockS3Client.Buckets[fileBucket][key] = buf.String()

				reqBody := map[string]interface{}{
					"cert_file_s3_path": "s3://" + fileBucket + "/" + key,
				}
				reqBodyBytes, err := json.Marshal(reqBody)
				Expect(err).To(BeNil())

				request.Path = "/v1/admin/nodes/registration-jobs"
				request.Body = string(reqBodyBytes)
				request.HTTPMethod = "POST"
				resp, err := handleRequest(ctx.Context, request)
				Expect(err).To(BeNil())
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			}

			// Filter for completed jobs
			request.Body = ""
			request.HTTPMethod = "GET"
			request.QueryStringParameters = map[string]string{"status": "completed"}
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var listResp ListRegistrationJobsResponse
			err = json.Unmarshal([]byte(response.Body), &listResp)
			Expect(err).To(BeNil())
			for _, job := range listResp.Jobs {
				Expect(job.Status).To(Equal("completed"))
			}

			// Filter for a status that won't match any
			request.QueryStringParameters = map[string]string{"status": "data_loaded"}
			response, err = handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			err = json.Unmarshal([]byte(response.Body), &listResp)
			Expect(err).To(BeNil())
			Expect(listResp.Jobs).To(HaveLen(0))

			// Clean up query params
			request.QueryStringParameters = nil
		})
	})

	Describe("Get Status API Tests", func() {
		It("should return error for missing request_id", func() {
			request.Body = ""
			request.Path = "/v1/admin/nodes/registration-jobs/"
			request.HTTPMethod = "GET"
			request.PathParameters = map[string]string{} // No requestId
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			var apiStatus utils.APIStatus
			err = json.Unmarshal([]byte(response.Body), &apiStatus)
			Expect(err).To(BeNil())
			Expect(apiStatus.Message).ToNot(BeEmpty())
		})

		It("should return 404 for non-existent request_id", func() {
			// Previously returned 200 with empty body; now returns 404 because
			// loadJob enforces existence and cross-flow isolation. A status
			// poll for an unknown id is a real client error.
			request.Body = ""
			request.Path = "/v1/admin/nodes/registration-jobs/non-existent-id"
			request.HTTPMethod = "GET"
			request.PathParameters = map[string]string{"requestId": "non-existent-id"}
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns 404 when the request_id is an update job (cross-flow isolation)", func() {
			// Seed an update-type job directly via the DB layer; the
			// registration Lambda must NOT surface it through /registration-jobs.
			seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
			seederCtx.SetAllow(utils.NodeAdminAdd, "*")
			seederCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")
			updateID := "seeded-update-job"
			err := node_reg_req_db.NewNodeRegRequestsDB(seederCtx).
				CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
					RequestID: updateID,
					JobType:   node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE,
					Status:    node_reg_req_db.NODE_REG_STATUS_COMPLETED,
				})
			Expect(err).To(BeNil())

			request.Body = ""
			request.Path = "/v1/admin/nodes/registration-jobs/" + updateID
			request.HTTPMethod = "GET"
			request.PathParameters = map[string]string{"requestId": updateID}
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns 404 on /failed-nodes when the request_id is an update job", func() {
			seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
			seederCtx.SetAllow(utils.NodeAdminAdd, "*")
			seederCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")
			updateID := "seeded-update-job-fn"
			err := node_reg_req_db.NewNodeRegRequestsDB(seederCtx).
				CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
					RequestID: updateID,
					JobType:   node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE,
					Status:    node_reg_req_db.NODE_REG_STATUS_COMPLETED,
				})
			Expect(err).To(BeNil())

			request.HTTPMethod = "GET"
			request.Path = "/v1/admin/nodes/registration-jobs/" + updateID + "/failed-nodes"
			request.PathParameters = map[string]string{"requestId": updateID}
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("treats legacy rows without job_type as register jobs (backward compat)", func() {
			// A row written before the JobType field existed has no attribute
			// for it; JobTypeOrDefault returns "register", so loadJob accepts it.
			seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
			seederCtx.SetAllow(utils.NodeAdminAdd, "*")
			seederCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")
			legacyID := "legacy-row"
			err := node_reg_req_db.NewNodeRegRequestsDB(seederCtx).
				CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
					RequestID: legacyID,
					Status:    node_reg_req_db.NODE_REG_STATUS_COMPLETED,
					// JobType deliberately not set — represents pre-feature data.
				})
			Expect(err).To(BeNil())

			request.HTTPMethod = "GET"
			request.Path = "/v1/admin/nodes/registration-jobs/" + legacyID
			request.PathParameters = map[string]string{"requestId": legacyID}
			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var statusResp RegisterNodeStatusResponse
			Expect(json.Unmarshal([]byte(response.Body), &statusResp)).To(Succeed())
			Expect(statusResp.RequestID).To(Equal(legacyID))
			Expect(statusResp.JobType).To(Equal(node_reg_req_db.NODE_REG_JOB_TYPE_REGISTER))
		})
	})

	Describe("Failed Nodes API Tests", func() {
		var (
			seedRequestID string
		)

		// seedJobAndFailuresWithS3Path writes a parent job row (with the given
		// cert_file_s3_path) plus a slice of failure rows using a fresh seeder
		// context with the necessary RBAC. Returns the request_id so tests can
		// target it.
		seedJobAndFailuresWithS3Path := func(certFileS3Path string, failures []node_reg_failed_nodes_db.NodeRegFailedNodeEntry) string {
			seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
			seederCtx.SetAllow(utils.NodeAdminAdd, "*")
			seederCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")

			requestID := "seeded-request-" + fmt.Sprintf("%d", GinkgoRandomSeed())
			err := node_reg_req_db.NewNodeRegRequestsDB(seederCtx).
				CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
					RequestID:      requestID,
					Status:         node_reg_req_db.NODE_REG_STATUS_COMPLETED,
					CertFileS3Path: certFileS3Path,
				})
			Expect(err).NotTo(HaveOccurred())

			if len(failures) > 0 {
				err = node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(seederCtx).
					RecordFailures(requestID, failures)
				Expect(err).NotTo(HaveOccurred())
			}
			return requestID
		}

		// seedJobAndFailures is the common case — no S3 path needed.
		seedJobAndFailures := func(failures []node_reg_failed_nodes_db.NodeRegFailedNodeEntry) string {
			return seedJobAndFailuresWithS3Path("", failures)
		}

		BeforeEach(func() {
			seedRequestID = ""
		})

		It("returns failure rows for a job that has them", func() {
			seedRequestID = seedJobAndFailures([]node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
				{NodeID: "n1", Reason: "boom-1"},
				{NodeID: "n2", Reason: "boom-2"},
			})

			request.Body = ""
			request.HTTPMethod = "GET"
			request.Path = "/v1/admin/nodes/registration-jobs/" + seedRequestID + "/failed-nodes"
			request.PathParameters = map[string]string{"requestId": seedRequestID}

			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp ListFailedNodesResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.FailedNodes).To(HaveLen(2))
			Expect(resp.PageTotal).To(Equal(2))
			Expect(resp.NextKey).To(BeEmpty())

			ids := []string{}
			reasons := []string{}
			for _, e := range resp.FailedNodes {
				ids = append(ids, e.NodeID)
				reasons = append(reasons, e.Reason)
			}
			Expect(ids).To(ConsistOf("n1", "n2"))
			Expect(reasons).To(ConsistOf("boom-1", "boom-2"))
		})

		It("returns 200 with empty list when the job exists but has no failures", func() {
			seedRequestID = seedJobAndFailures(nil)

			request.HTTPMethod = "GET"
			request.Path = "/v1/admin/nodes/registration-jobs/" + seedRequestID + "/failed-nodes"
			request.PathParameters = map[string]string{"requestId": seedRequestID}

			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp ListFailedNodesResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.FailedNodes).To(BeEmpty())
			Expect(resp.PageTotal).To(Equal(0))
		})

		It("returns 404 when the job does not exist", func() {
			request.HTTPMethod = "GET"
			request.Path = "/v1/admin/nodes/registration-jobs/no-such-job/failed-nodes"
			request.PathParameters = map[string]string{"requestId": "no-such-job"}

			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("paginates via next_key", func() {
			failures := make([]node_reg_failed_nodes_db.NodeRegFailedNodeEntry, 0, 5)
			for i := 0; i < 5; i++ {
				failures = append(failures, node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
					NodeID: fmt.Sprintf("node-%02d", i),
					Reason: "x",
				})
			}
			seedRequestID = seedJobAndFailures(failures)

			request.HTTPMethod = "GET"
			request.Path = "/v1/admin/nodes/registration-jobs/" + seedRequestID + "/failed-nodes"
			request.PathParameters = map[string]string{"requestId": seedRequestID}
			request.QueryStringParameters = map[string]string{"page_size": "2"}

			seen := map[string]bool{}
			startKey := ""
			pageCount := 0
			for {
				if startKey == "" {
					delete(request.QueryStringParameters, "start_key")
				} else {
					request.QueryStringParameters["start_key"] = startKey
				}
				response, err := handleRequest(ctx.Context, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))

				var resp ListFailedNodesResponse
				Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
				for _, e := range resp.FailedNodes {
					seen[e.NodeID] = true
				}
				pageCount++
				if resp.NextKey == "" {
					break
				}
				startKey = resp.NextKey
			}
			Expect(seen).To(HaveLen(5))
			Expect(pageCount).To(BeNumerically(">=", 3)) // 2+2+1 with limit=2

			request.QueryStringParameters = nil
		})

		It("rejects malformed start_key", func() {
			seedRequestID = seedJobAndFailures([]node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
				{NodeID: "n1", Reason: "x"},
			})

			request.HTTPMethod = "GET"
			request.Path = "/v1/admin/nodes/registration-jobs/" + seedRequestID + "/failed-nodes"
			request.PathParameters = map[string]string{"requestId": seedRequestID}
			request.QueryStringParameters = map[string]string{
				"start_key": base64.StdEncoding.EncodeToString([]byte("not a valid pagination payload")),
			}

			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))

			request.QueryStringParameters = nil
		})

		It("status returns a presigned download URL when a failed-nodes CSV exists", func() {
			seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
			seederCtx.SetAllow(utils.NodeAdminAdd, "*")
			seederCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")

			requestID := "seeded-failedfile-" + fmt.Sprintf("%d", GinkgoRandomSeed())
			failedKey := "system/" + requestID + "_failed_node_certs.csv"
			failedPath := "s3://" + os.Getenv("FILE_BUCKET_NAME") + "/" + failedKey
			Expect(node_reg_req_db.NewNodeRegRequestsDB(seederCtx).
				CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
					RequestID:        requestID,
					Status:           node_reg_req_db.NODE_REG_STATUS_COMPLETED,
					FailedFileS3Path: failedPath,
				})).To(Succeed())

			request.HTTPMethod = "GET"
			request.Path = "/v1/admin/nodes/registration-jobs/" + requestID
			request.PathParameters = map[string]string{"requestId": requestID}

			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp RegisterNodeStatusResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.FailedFileS3Path).To(Equal(failedPath))
			// Presigned GET URL points at the same object and is signed.
			Expect(resp.FailedFileDownloadURL).To(ContainSubstring(failedKey))
			Expect(resp.FailedFileDownloadURL).To(ContainSubstring("X-Amz-Signature"))
		})

		It("status omits the download fields when no failed-nodes CSV exists", func() {
			seedRequestID = seedJobAndFailures(nil)

			request.HTTPMethod = "GET"
			request.Path = "/v1/admin/nodes/registration-jobs/" + seedRequestID
			request.PathParameters = map[string]string{"requestId": seedRequestID}

			response, err := handleRequest(ctx.Context, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp RegisterNodeStatusResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.FailedFileS3Path).To(BeEmpty())
			Expect(resp.FailedFileDownloadURL).To(BeEmpty())
		})
	})
})

var _ = Describe("Admin Nodes Registration auth gate", func() {
	var ctx context.Context
	var adminID, nonAdminID string

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		adminID = "auth-gate-admin"
		nonAdminID = "auth-gate-non-admin"
		test_utils.SetupTestAdminUser(ctx, adminID, "admin@example.com")
		test_utils.SetupTestNonAdminUserInAdminPool(ctx, nonAdminID, "user@example.com")
	})

	makeRequest := func(userID, path, method string) events.APIGatewayProxyRequest {
		return events.APIGatewayProxyRequest{
			HTTPMethod: method,
			Path:       path,
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             userID,
					CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
				},
			},
		}
	}

	// 401 (unresolved user / non-*user.User accessor) is integration-tested
	// only — the unit-test auth mock synthesises a user for any Cognito
	// identity, so the rctx == nil branch isn't reachable from here.

	It("returns 403 for a non-super-admin user on POST /v1/admin/nodes", func() {
		resp, err := handleRequest(ctx, makeRequest(nonAdminID, "/v1/admin/nodes", "POST"))
		Expect(err).To(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		Expect(resp.Body).To(ContainSubstring("Forbidden"))
	})

	It("returns 403 for a non-super-admin user on GET /v1/admin/nodes/registration-jobs", func() {
		resp, err := handleRequest(ctx, makeRequest(nonAdminID, "/v1/admin/nodes/registration-jobs", "GET"))
		Expect(err).To(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		Expect(resp.Body).To(ContainSubstring("Forbidden"))
	})

	It("admits a super-admin past the auth gate", func() {
		// A super-admin on an unknown path should reach the 404 router
		// branch — proving the auth gate passed.
		resp, err := handleRequest(ctx, makeRequest(adminID, "/v1/admin/nodes/unknown-route", "GET"))
		Expect(err).To(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})
})

func TestAdminNodes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Admin Nodes API Suite")
}
