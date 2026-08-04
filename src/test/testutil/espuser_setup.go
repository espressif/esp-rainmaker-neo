// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/onsi/gomega"
)

// Default SSM/issuer values shared by the espuser unit tests. Individual specs can override any of these via EspUserBackendOpts when they historically used different literals.
const (
	EspUserKMSKeyARN          = "arn:aws:kms:test:000000000000:key/espuser-test"
	EspUserJWKSParam          = "/espuser/base/user-jwks-test"
	EspUserRefreshSecretParam = "/espuser/signing/refresh-test"
	EspUserRefreshSecret      = "test-refresh-hmac-secret"
	EspUserIssuer             = "https://issuer.test"
)

// EspUserBackendOpts tunes SetupEspUserBackend. A zero value yields the common defaults.
type EspUserBackendOpts struct {
	Issuer             string
	RefreshSecretParam string
	RefreshSecret      string
	WithJWKS           bool // start an httptest JWKS server and point Issuer at it
	WithSES            bool // install a MockSES client (for dispatch/sender specs)
}

// EspUserBackend is the installed mock backend: the DynamoDB/SSM/SES mocks, the generated signing key and the resolved env values. Tests read what they need and defer Close.
type EspUserBackend struct {
	DBMock             *mock.DynamoDBMock
	SSMMock            *mock.MockSSM
	SESMock            *mock.MockSES
	SigningKey         *rsa.PrivateKey
	Issuer             string
	RefreshSecretParam string
	RefreshSecret      string
	jwksServer         *httptest.Server
}

func SetupEspUserBackend(ctx context.Context, opts ...EspUserBackendOpts) *EspUserBackend {
	opt := EspUserBackendOpts{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	b := &EspUserBackend{
		Issuer:             firstNonEmpty(opt.Issuer, EspUserIssuer),
		RefreshSecretParam: firstNonEmpty(opt.RefreshSecretParam, EspUserRefreshSecretParam),
		RefreshSecret:      firstNonEmpty(opt.RefreshSecret, EspUserRefreshSecret),
	}

	b.DBMock = mock.NewDynamoDBMock()
	awscommon.SetDynamoDBClient(b.DBMock)
	seedEspUserSchema(b.DBMock)

	if opt.WithSES {
		b.SESMock = mock.NewMockSES()
		awscommon.SetSESClient(b.SESMock)
	}

	b.SSMMock = mock.NewMockSSM()
	awscommon.SetSSMClient(b.SSMMock)

	// One shared signing key across the suite (see cognito_utils.SigningKey).
	priv := TestJWKUtil.SigningKey()
	b.SigningKey = priv

	awscommon.SetKMSClient(newRSAKMSWithKey(EspUserKMSKeyARN, priv))
	os.Setenv("ESPUSER_KMS_SIGNING_KEY_ARN", EspUserKMSKeyARN)

	PutParam(ctx, b.SSMMock, b.RefreshSecretParam, b.RefreshSecret)
	os.Setenv("ESPUSER_REFRESH_SECRET_PARAM", b.RefreshSecretParam)
	os.Unsetenv("SSM_" + strings.ToUpper(b.RefreshSecretParam))

	if opt.WithJWKS {
		b.jwksServer = startJWKSServer(&priv.PublicKey)
		b.Issuer = b.jwksServer.URL
	}

	seedOIDCKeys(ctx, b.SSMMock, priv, b.Issuer)

	return b
}

// Close tears down the JWKS server (if any) and drops the per-process JWKS cache so a later spec is not served this run's key. Safe to call when no JWKS server was started.
func (b *EspUserBackend) Close() {
	if b.jwksServer != nil {
		b.jwksServer.Close()
		jwk.NewCache(context.Background())
	}
}

// seedEspUserSchema seeds the full espuser table/GSI set by literal name/key so the helper stays free of espuser db-package imports (avoids an import cycle). Tests use what they need.
func seedEspUserSchema(m *mock.DynamoDBMock) {
	m.AddTable("espuser-otp", "otp_id", "")
	m.AddTable("espuser-refresh-tokens", "user_id", "client_family")
	m.AddTable("espuser-user-details", "user_id", "")
	m.AddTable("espuser-oauth-clients", "client_id", "")
	m.AddTable("espuser-auth-flows", "flow_id", "")
	m.AddTable("espuser-admin-config", "config_name", "subtype")
	m.AddTable("espuser-identity-providers", "provider_name", "")
	gomega.Expect(m.AddSecondaryIndex("espuser-user-details-by-email", "espuser-user-details", "email", "")).To(gomega.Succeed())
	gomega.Expect(m.AddSecondaryIndex("espuser-user-details-by-phone", "espuser-user-details", "phone", "")).To(gomega.Succeed())
	gomega.Expect(m.AddSecondaryIndex("espuser-auth-flows-by-code", "espuser-auth-flows", "code", "")).To(gomega.Succeed())
}

func startJWKSServer(pub *rsa.PublicKey) *httptest.Server {
	set := jwtutil.BuildJWKS(jwtutil.BuildJWK(pub, oidc.SigningKeyID), jwtutil.BuildJWK(pub, jwtutil.RSAThumbprint(pub)))
	body, err := json.Marshal(set)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	mux := http.NewServeMux()
	mux.HandleFunc(oidc.JWKSPath, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})
	return httptest.NewServer(mux)
}

func PutParam(ctx context.Context, m *mock.MockSSM, name, value string) {
	gomega.Expect(putParam(ctx, m, name, value)).To(gomega.Succeed())
}

// Overwrite, unlike production writes: seeding a fixture replaces whatever an earlier
// spec left behind, and the mock refuses an overwrite-less write to an existing name.
func putParam(ctx context.Context, m *mock.MockSSM, name, value string) error {
	_, err := m.PutParameter(ctx, &ssm.PutParameterInput{
		Name: &name, Value: &value, Type: ssmtypes.ParameterTypeSecureString,
		Overwrite: aws.Bool(true),
	})
	return err
}

// Reports failure by panicking rather than through gomega: TestSetup calls this, and it
// runs from plain go tests as well, where an assertion has no registered fail handler.
func seedOIDCKeys(ctx context.Context, m *mock.MockSSM, priv *rsa.PrivateKey, issuer string) {
	jwksJSON, err := json.Marshal(jwtutil.BuildJWKS(
		jwtutil.BuildJWK(&priv.PublicKey, oidc.SigningKeyID),
		jwtutil.BuildJWK(&priv.PublicKey, jwtutil.RSAThumbprint(&priv.PublicKey)),
	))
	if err != nil {
		panic(fmt.Sprintf("failed to marshal the test JWKS: %v", err))
	}
	if err := putParam(ctx, m, EspUserJWKSParam, string(jwksJSON)); err != nil {
		panic(fmt.Sprintf("failed to store %s in the SSM mock: %v", EspUserJWKSParam, err))
	}

	os.Setenv("USER_ISSUER", issuer)
	os.Setenv("USER_JWKS_PARA_NAME", EspUserJWKSParam)
	os.Unsetenv("SSM_" + strings.ToUpper(EspUserJWKSParam))
}

// newRSAKMSWithKey installs the suite's own signing key in the KMS mock, so a token minted
// here verifies against the JWKS the same key builds.
func newRSAKMSWithKey(keyID string, key *rsa.PrivateKey) *mock.MockKMS {
	m := mock.NewMockKMS()
	m.AddRSAKeyWith(keyID, key)
	return m
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
