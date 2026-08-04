// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package push

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetPlatformApplicationAttributes", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
	})

	It("strips private_key from a stored Google service-account credential but keeps its metadata", func() {
		saJSON := `{"type":"service_account","project_id":"test-project","private_key_id":"key-id-1","private_key":"-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----\n","client_email":"push@test-project.iam.gserviceaccount.com"}`
		Expect(UpdatePlatformApplication(ctx, "GCM", "test-project", map[string]string{"PlatformCredential": saJSON})).To(Succeed())

		attrs, err := GetPlatformApplicationAttributes(ctx, "GCM", "test-project")
		Expect(err).To(BeNil())

		var sa map[string]interface{}
		Expect(json.Unmarshal([]byte(attrs["PlatformCredential"]), &sa)).To(Succeed())
		Expect(sa).NotTo(HaveKey("private_key"))
		Expect(sa).To(HaveKeyWithValue("project_id", "test-project"))
		Expect(sa).To(HaveKeyWithValue("private_key_id", "key-id-1"))
		Expect(sa).To(HaveKeyWithValue("client_email", "push@test-project.iam.gserviceaccount.com"))
	})

	It("drops a credential that is wholly secret, like an APNS signing key", func() {
		stored := map[string]string{
			"PlatformCredential":    "-----BEGIN PRIVATE KEY-----\np8-secret\n-----END PRIVATE KEY-----",
			"ApplePlatformBundleID": "com.test.app",
		}
		Expect(UpdatePlatformApplication(ctx, "APNS", "TestApp", stored)).To(Succeed())

		attrs, err := GetPlatformApplicationAttributes(ctx, "APNS", "TestApp")
		Expect(err).To(BeNil())
		Expect(attrs).NotTo(HaveKey("PlatformCredential"))
		Expect(attrs).To(HaveKeyWithValue("ApplePlatformBundleID", "com.test.app"))
	})

	It("wraps SNS errors", func() {
		awscommon.GetSNSClient().(*mock.SNSMock).GetPlatformApplicationAttributesError = fmt.Errorf("simulated error")

		attrs, err := GetPlatformApplicationAttributes(ctx, "APNS", "TestApp")
		Expect(err).To(HaveOccurred())
		Expect(attrs).To(BeNil())
	})
})
