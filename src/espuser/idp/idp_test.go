// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package idp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/pkceutil"

	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIDP(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IDP Suite")
}

var _ = Describe("upstream state (HMAC)", func() {
	key := StateHMACKey([]byte("refresh-secret"))

	It("round-trips the flow id", func() {
		s := encodeState("flow-abc", key)
		got, err := decodeState(s, key)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("flow-abc"))
	})

	It("rejects a tampered tag", func() {
		s := encodeState("flow-abc", key)
		_, err := decodeState(s+"x", key)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a state signed with a different key", func() {
		s := encodeState("flow-abc", StateHMACKey([]byte("other")))
		_, err := decodeState(s, key)
		Expect(err).To(HaveOccurred())
	})

	It("derives distinct keys from the refresh secret (domain separation)", func() {
		Expect(StateHMACKey([]byte("s"))).NotTo(Equal([]byte("s")))
	})
})

func signedIDToken(priv *rsa.PrivateKey, claims jwt.MapClaims) (string, string) {
	kid := jwtutil.RSAThumbprint(&priv.PublicKey)
	tok, err := jwtutil.SignRS256(claims, priv, kid)
	Expect(err).NotTo(HaveOccurred())
	jwks := jwtutil.BuildJWKS(jwtutil.BuildJWK(&priv.PublicKey, kid))
	b, err := json.Marshal(jwks)
	Expect(err).NotTo(HaveOccurred())
	return tok, string(b)
}

var _ = Describe("Identity.VerifiedContacts", func() {
	It("returns both contacts when the upstream verified both", func() {
		email, phone, err := Identity{
			Email: "ada@example.com", EmailVerified: true,
			PhoneNumber: "+15550100", PhoneVerified: true,
		}.VerifiedContacts()
		Expect(err).NotTo(HaveOccurred())
		Expect(email).To(Equal("ada@example.com"))
		Expect(phone).To(Equal("+15550100"))
	})

	It("drops an unverified email but keeps the verified phone", func() {
		email, phone, err := Identity{
			Email: "ada@example.com", PhoneNumber: "+15550100", PhoneVerified: true,
		}.VerifiedContacts()
		Expect(err).NotTo(HaveOccurred())
		Expect(email).To(BeEmpty(), "an unverified email must not identify an account")
		Expect(phone).To(Equal("+15550100"))
	})

	It("drops an unverified phone but keeps the verified email", func() {
		email, phone, err := Identity{
			Email: "ada@example.com", EmailVerified: true, PhoneNumber: "+15550100",
		}.VerifiedContacts()
		Expect(err).NotTo(HaveOccurred())
		Expect(email).To(Equal("ada@example.com"))
		Expect(phone).To(BeEmpty(), "an unverified phone must not identify an account")
	})

	It("refuses an identity with nothing verified (negative)", func() {
		_, _, err := Identity{Email: "ada@example.com", PhoneNumber: "+15550100"}.VerifiedContacts()
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("OIDCProvider.AuthorizeRedirectURL", func() {
	base := func(authorizeURL string) *OIDCProvider {
		return &OIDCProvider{
			ProviderName: "up", AuthorizeURL: authorizeURL,
			ClientID: "broker", CallbackURL: "https://us.example/oauth2/federation/callback",
			Scopes: []string{"openid", "email"},
		}
	}
	leg := UpstreamLeg{State: "st", Nonce: "no", PKCEVerifier: "ver"}

	It("carries our state, nonce and an S256 challenge", func() {
		got, err := base("https://up.example/authorize").AuthorizeRedirectURL(context.Background(), leg)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(ContainSubstring("state=st"))
		Expect(got).To(ContainSubstring("nonce=no"))
		Expect(got).To(ContainSubstring("code_challenge_method=S256"))
		Expect(got).To(ContainSubstring("code_challenge=" + pkceutil.ChallengeS256("ver")))
	})

	It("appends with & when the pinned authorize URL already has a query", func() {
		got, err := base("https://up.example/authorize?p=policy").AuthorizeRedirectURL(context.Background(), leg)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HavePrefix("https://up.example/authorize?p=policy&"))
		Expect(got).NotTo(ContainSubstring("?p=policy?"), "a second ? would make the URL malformed")
	})

	It("refuses a misconfigured provider (negative)", func() {
		_, err := base("").AuthorizeRedirectURL(context.Background(), leg)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("OIDCProvider.HandleCallback", func() {
	const (
		issuer   = "https://cognito-idp.eu-west-2.amazonaws.com/pool"
		clientID = "broker-client"
		nonce    = "nonce-xyz"
	)
	var priv *rsa.PrivateKey

	BeforeEach(func() {
		var err error
		priv, err = rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).NotTo(HaveOccurred())
	})

	provider := func(idToken, jwks string) *OIDCProvider {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"id_token": idToken})
		}))
		DeferCleanup(srv.Close)
		return &OIDCProvider{
			ProviderName: "cognito", Issuer: issuer, ClientID: clientID,
			CallbackURL: "https://api/oauth2/federation/callback", JWKS: jwks, TokenURL: srv.URL,
		}
	}

	baseClaims := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss": issuer, "aud": clientID, "sub": "cog-sub-1", "nonce": nonce,
			"email": "u@example.com", "email_verified": true,
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		}
	}

	It("carries allow-listed profile claims into the identity", func() {
		claims := baseClaims()
		claims["name"] = "Ada Lovelace"
		claims["locale"] = "en-GB"
		claims["picture"] = "https://img.example/ada.png"
		tok, jwks := signedIDToken(priv, claims)
		id, err := provider(tok, jwks).HandleCallback(context.Background(), "code", UpstreamLeg{Nonce: nonce})
		Expect(err).NotTo(HaveOccurred())
		Expect(id.Name).To(Equal("Ada Lovelace"))
		Expect(id.Locale).To(Equal("en-GB"))
		Expect(id.Picture).To(Equal("https://img.example/ada.png"))
	})

	It("honors the registry attribute mapping over the default claim names", func() {
		claims := baseClaims()
		claims["custom:display_name"] = "Mapped Name"
		tok, jwks := signedIDToken(priv, claims)
		p := provider(tok, jwks)
		p.AttributeMapping = map[string]string{"name": "custom:display_name"}
		id, err := p.HandleCallback(context.Background(), "code", UpstreamLeg{Nonce: nonce})
		Expect(err).NotTo(HaveOccurred())
		Expect(id.Name).To(Equal("Mapped Name"))
	})

	It("reads only allow-listed keys — a mapping cannot smuggle arbitrary claims (negative)", func() {
		claims := baseClaims()
		claims["custom:role"] = "superadmin"
		tok, jwks := signedIDToken(priv, claims)
		p := provider(tok, jwks)
		p.AttributeMapping = map[string]string{"role": "custom:role"}
		id, err := p.HandleCallback(context.Background(), "code", UpstreamLeg{Nonce: nonce})
		Expect(err).NotTo(HaveOccurred())
		Expect(id.Name).To(BeEmpty())
	})

	It("tolerates a string-typed email_verified (provider quirk)", func() {
		claims := baseClaims()
		claims["email_verified"] = "true"
		tok, jwks := signedIDToken(priv, claims)
		id, err := provider(tok, jwks).HandleCallback(context.Background(), "code", UpstreamLeg{Nonce: nonce})
		Expect(err).NotTo(HaveOccurred())
		Expect(id.EmailVerified).To(BeTrue())
	})

	It("returns the normalized verified identity on a valid id_token", func() {
		tok, jwks := signedIDToken(priv, baseClaims())
		id, err := provider(tok, jwks).HandleCallback(context.Background(), "code", UpstreamLeg{Nonce: nonce})
		Expect(err).NotTo(HaveOccurred())
		Expect(id.Email).To(Equal("u@example.com"))
		Expect(id.EmailVerified).To(BeTrue())
		Expect(id.ExternalSub).To(Equal("cog-sub-1"))
	})

	It("rejects a nonce mismatch (replay/injection guard)", func() {
		tok, jwks := signedIDToken(priv, baseClaims())
		_, err := provider(tok, jwks).HandleCallback(context.Background(), "code", UpstreamLeg{Nonce: "different"})
		Expect(err).To(HaveOccurred())
	})

	It("rejects an audience mismatch (token for another client)", func() {
		c := baseClaims()
		c["aud"] = "someone-else"
		tok, jwks := signedIDToken(priv, c)
		_, err := provider(tok, jwks).HandleCallback(context.Background(), "code", UpstreamLeg{Nonce: nonce})
		Expect(err).To(HaveOccurred())
	})

	It("rejects an issuer mismatch (mix-up defense)", func() {
		c := baseClaims()
		c["iss"] = "https://evil.example"
		tok, jwks := signedIDToken(priv, c)
		_, err := provider(tok, jwks).HandleCallback(context.Background(), "code", UpstreamLeg{Nonce: nonce})
		Expect(err).To(HaveOccurred())
	})

	It("rejects a token signed by an unknown key", func() {
		other, _ := rsa.GenerateKey(rand.Reader, 2048)
		tok, _ := signedIDToken(priv, baseClaims())
		_, jwks := signedIDToken(other, baseClaims()) // JWKS for a different key
		_, err := provider(tok, jwks).HandleCallback(context.Background(), "code", UpstreamLeg{Nonce: nonce})
		Expect(err).To(HaveOccurred())
	})
})
