// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmngrequest

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TestRequest is a test struct to use for testing sanitization
type TestRequest struct {
	Name     string   `json:"name" validate:"required"`
	Age      int      `json:"age" validate:"required,min=18"`
	Email    string   `json:"email"`
	IsActive bool     `json:"is_active"`
	Tags     []string `json:"tags"`
}

var _ = Describe("rmng request", func() {
	var (
		request      events.APIGatewayProxyRequest
		inputRequest TestRequest
	)

	BeforeEach(func() {
		// Reset request for each test
		request = events.APIGatewayProxyRequest{}
		inputRequest = TestRequest{}
	})

	It("should process base64 encoded body", func() {
		// Create test data
		expected := TestRequest{
			Name:     "Test User",
			Age:      30,
			Email:    "test@example.com",
			IsActive: true,
			Tags:     []string{"tag1", "tag2"},
		}

		// Marshal to JSON
		jsonData, err := json.Marshal(expected)
		Expect(err).To(BeNil())

		// Base64 encode the JSON
		encodedBody := base64.StdEncoding.EncodeToString(jsonData)

		// Create API Gateway request with base64 encoded body
		request := events.APIGatewayProxyRequest{
			Body:            encodedBody,
			IsBase64Encoded: true,
		}

		// Test the function
		var actual TestRequest
		err = GetRequest(request, &actual)
		Expect(err).To(BeNil())

		// Verify the result
		Expect(actual).To(Equal(expected))
	})

	It("should process and transform the request", func() {
		// Create test data
		input := TestRequest{
			Name:     "Test User",
			Age:      30,
			Email:    "test@example.com",
			IsActive: true,
			Tags:     []string{"tag1", "tag2"},
		}

		// Marshal to JSON
		jsonData, err := json.Marshal(input)
		Expect(err).To(BeNil())

		// Create API Gateway request
		request := events.APIGatewayProxyRequest{
			Body: string(jsonData),
		}

		// Test the function
		var inputReq TestRequest
		err = ExtractRequestStruct(request, &inputReq)
		Expect(err).To(BeNil())

		// Verify the result
		Expect(inputReq).To(Equal(input))
	})

	It("should fail validation when required fields are missing", func() {
		input := TestRequest{
			Name: "Test User",
			Age:  15,
		}
		jsonData, err := json.Marshal(input)
		Expect(err).To(BeNil())

		request = events.APIGatewayProxyRequest{
			Body: string(jsonData),
		}

		err = ExtractRequestStruct(request, &inputRequest)
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("failed to validate request"))
	})
})

var _ = Describe("RedactForLog", func() {
	It("should redact the Authorization header", func() {
		request := events.APIGatewayProxyRequest{
			Headers: map[string]string{"Authorization": "Bearer secret-token"},
		}

		redacted := RedactForLog(request)
		Expect(redacted.Headers["Authorization"]).To(Equal("[REDACTED]"))
	})

	It("should redact the lowercase authorization header", func() {
		request := events.APIGatewayProxyRequest{
			Headers: map[string]string{"authorization": "Bearer secret-token"},
		}

		redacted := RedactForLog(request)
		Expect(redacted.Headers["authorization"]).To(Equal("[REDACTED]"))
	})

	It("should redact the Authorization multi-value header", func() {
		request := events.APIGatewayProxyRequest{
			MultiValueHeaders: map[string][]string{"Authorization": {"Bearer secret-token"}},
		}

		redacted := RedactForLog(request)
		Expect(redacted.MultiValueHeaders["Authorization"]).To(Equal([]string{"[REDACTED]"}))
	})

	It("should redact secrets carried in a JSON body", func() {
		// /v1/user/credentials takes an id_token in the body, and the password and
		// signout endpoints take access/refresh tokens plus cleartext passwords, so
		// header-only redaction would still leak all of them.
		request := events.APIGatewayProxyRequest{
			Body: `{"id_token":"secret-id","access_token":"secret-access","refresh_token":"secret-refresh","old_password":"pw1","new_password":"pw2","global":"true"}`,
		}

		redacted := RedactForLog(request)

		for _, secret := range []string{"secret-id", "secret-access", "secret-refresh", "pw1", "pw2"} {
			Expect(redacted.Body).NotTo(ContainSubstring(secret))
		}
		// Non-secret fields survive, so the log line stays useful.
		Expect(redacted.Body).To(ContainSubstring(`"global":"true"`))
	})

	It("should leave a body with no secrets untouched", func() {
		request := events.APIGatewayProxyRequest{Body: `{"group_id":"abc123"}`}

		redacted := RedactForLog(request)
		Expect(redacted.Body).To(Equal(`{"group_id":"abc123"}`))
	})

	It("should leave a non-JSON body untouched rather than mangle it", func() {
		request := events.APIGatewayProxyRequest{Body: "not json at all"}

		redacted := RedactForLog(request)
		Expect(redacted.Body).To(Equal("not json at all"))
	})

	It("should leave non-authorization headers untouched", func() {
		request := events.APIGatewayProxyRequest{
			Headers:           map[string]string{"Content-Type": "application/json", "Authorization": "Bearer secret-token"},
			MultiValueHeaders: map[string][]string{"Accept": {"application/json"}},
		}

		redacted := RedactForLog(request)
		Expect(redacted.Headers["Content-Type"]).To(Equal("application/json"))
		Expect(redacted.MultiValueHeaders["Accept"]).To(Equal([]string{"application/json"}))
	})

	It("should not mutate the original request", func() {
		request := events.APIGatewayProxyRequest{
			Headers:           map[string]string{"Authorization": "Bearer secret-token"},
			MultiValueHeaders: map[string][]string{"Authorization": {"Bearer secret-token"}},
		}

		_ = RedactForLog(request)
		Expect(request.Headers["Authorization"]).To(Equal("Bearer secret-token"))
		Expect(request.MultiValueHeaders["Authorization"]).To(Equal([]string{"Bearer secret-token"}))
	})

	It("should handle a request with no headers", func() {
		request := events.APIGatewayProxyRequest{}

		redacted := RedactForLog(request)
		Expect(redacted.Headers).To(BeNil())
		Expect(redacted.MultiValueHeaders).To(BeNil())
	})
})

func TestRmngRequest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "rmng request Suite")
}
