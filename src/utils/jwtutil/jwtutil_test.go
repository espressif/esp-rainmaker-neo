// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package jwtutil_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestJWTUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "JWTUtils Suite")
}

var _ = Describe("JWK builders", func() {
	var pub *rsa.PublicKey
	BeforeEach(func() {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).NotTo(HaveOccurred())
		pub = &priv.PublicKey
	})

	Describe("BuildJWK", func() {
		It("emits the required RSA verify-key members", func() {
			jwk := jwtutil.BuildJWK(pub, "2026-06-a")
			Expect(jwk.Kty).To(Equal("RSA"))
			Expect(jwk.Use).To(Equal("sig"))
			Expect(jwk.Alg).To(Equal("RS256"))
			Expect(jwk.Kid).To(Equal("2026-06-a"))
			Expect(jwk.N).NotTo(BeEmpty())
			Expect(jwk.E).NotTo(BeEmpty())
		})

		It("encodes n and e as unpadded base64url that round-trips to the key", func() {
			jwk := jwtutil.BuildJWK(pub, "k1")

			nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
			Expect(err).NotTo(HaveOccurred())
			Expect(new(big.Int).SetBytes(nBytes)).To(Equal(pub.N))

			eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
			Expect(err).NotTo(HaveOccurred())
			Expect(int(new(big.Int).SetBytes(eBytes).Int64())).To(Equal(pub.E))
		})

		It("never leaks private material (only public members are set)", func() {
			jwk := jwtutil.BuildJWK(pub, "k1")
			Expect(jwk.N).NotTo(ContainSubstring("PRIVATE"))
		})
	})

	Describe("BuildJWKS", func() {
		It("wraps a single active key (v1)", func() {
			set := jwtutil.BuildJWKS(jwtutil.BuildJWK(pub, "k1"))
			Expect(set.Keys).To(HaveLen(1))
			Expect(set.Keys[0].Kid).To(Equal("k1"))
		})

		It("supports an overlap set for future rotation", func() {
			set := jwtutil.BuildJWKS(jwtutil.BuildJWK(pub, "k1"), jwtutil.BuildJWK(pub, "k2"))
			Expect(set.Keys).To(HaveLen(2))
		})

		It("yields an empty (not nil-panicking) set when no keys are given", func() {
			set := jwtutil.BuildJWKS()
			Expect(set.Keys).To(BeEmpty())
		})
	})
})

var _ = Describe("UnverifiedIssuer", func() {
	It("returns the iss claim without a signing key", func() {
		token := unsignedToken(jwt.MapClaims{"iss": "https://issuer.test", "sub": "u1"})
		iss, err := jwtutil.UnverifiedIssuer(token)
		Expect(err).NotTo(HaveOccurred())
		Expect(iss).To(Equal("https://issuer.test"))
	})

	It("errors on a malformed token", func() {
		_, err := jwtutil.UnverifiedIssuer("not.a.jwt")
		Expect(err).To(HaveOccurred())
	})

	It("errors when the iss claim is absent", func() {
		token := unsignedToken(jwt.MapClaims{"sub": "u1"})
		_, err := jwtutil.UnverifiedIssuer(token)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ClassifyIssuer", func() {
	const esp = "https://issuer.test"

	It("routes the configured ESP issuer (trailing slash tolerant)", func() {
		Expect(jwtutil.ClassifyIssuer(esp, esp)).To(Equal(jwtutil.IssuerESPUser))
		Expect(jwtutil.ClassifyIssuer(esp+"/", esp)).To(Equal(jwtutil.IssuerESPUser))
	})

	It("routes any Cognito user-pool issuer", func() {
		Expect(jwtutil.ClassifyIssuer("https://cognito-idp.us-east-1.amazonaws.com/us-east-1_x", esp)).To(Equal(jwtutil.IssuerCognito))
	})

	It("returns unknown for an untrusted issuer", func() {
		Expect(jwtutil.ClassifyIssuer("https://evil.example", esp)).To(Equal(jwtutil.IssuerUnknown))
	})

	It("never matches ESP when ESPUSER_ISSUER is unset (negative)", func() {
		Expect(jwtutil.ClassifyIssuer(esp, "")).To(Equal(jwtutil.IssuerUnknown))
	})
})

func unsignedToken(claims jwt.MapClaims) string {
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(time.Hour).Unix()
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	Expect(err).NotTo(HaveOccurred())
	return signed
}

var _ = Describe("OIDC verify (ParseJWKS + VerifyJWT + AssertOIDCClaims)", func() {
	const kid = "k1"
	const issuer = "https://issuer.test"
	var (
		priv *rsa.PrivateKey
		jwks string
	)

	BeforeEach(func() {
		var err error
		priv, err = rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).NotTo(HaveOccurred())
		body, err := json.Marshal(jwtutil.BuildJWKS(jwtutil.BuildJWK(&priv.PublicKey, kid)))
		Expect(err).NotTo(HaveOccurred())
		jwks = string(body)
	})

	sign := func(claims jwt.MapClaims) string {
		token, err := jwtutil.SignRS256(claims, priv, kid)
		Expect(err).NotTo(HaveOccurred())
		return token
	}

	verify := jwtutil.VerifyOIDCToken

	It("verifies a well-formed token and returns its claims", func() {
		token := sign(jwt.MapClaims{"iss": issuer, "sub": "u1", "exp": time.Now().Add(time.Hour).Unix()})
		claims, err := verify(jwks, issuer, token)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims["sub"]).To(Equal("u1"))
	})

	It("rejects a token whose iss does not match the expected issuer (negative)", func() {
		token := sign(jwt.MapClaims{"iss": "https://evil.test", "sub": "u1", "exp": time.Now().Add(time.Hour).Unix()})
		_, err := verify(jwks, issuer, token)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a token with no expiry (negative)", func() {
		token := sign(jwt.MapClaims{"iss": issuer, "sub": "u1"})
		_, err := verify(jwks, issuer, token)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a token with no subject (negative)", func() {
		token := sign(jwt.MapClaims{"iss": issuer, "exp": time.Now().Add(time.Hour).Unix()})
		_, err := verify(jwks, issuer, token)
		Expect(err).To(HaveOccurred())
	})

	It("rejects an expired token (negative)", func() {
		token := sign(jwt.MapClaims{"iss": issuer, "sub": "u1", "exp": time.Now().Add(-time.Hour).Unix()})
		_, err := verify(jwks, issuer, token)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a malformed JWKS (negative)", func() {
		token := sign(jwt.MapClaims{"iss": issuer, "sub": "u1", "exp": time.Now().Add(time.Hour).Unix()})
		_, err := verify("not-json", issuer, token)
		Expect(err).To(HaveOccurred())
	})
})

// JWT forgery resistance. These are the highest-signal JWT attacks from RFC 9700 §4 and the OWASP
// JWT cheat sheet: an unsigned token, algorithm confusion (RS256->HS256 using the public key as an
// HMAC secret), an unknown/forged kid, a token signed by an attacker key, and payload tampering.
// The defense lives in rs256KeyFunc (confines the accepted algorithm to RSA and looks kid up in the
// JWK set); these lock it in so a refactor can't silently reopen the hole.
var _ = Describe("JWT forgery resistance (RFC 9700 / OWASP JWT)", func() {
	const kid = "k1"
	const issuer = "https://issuer.test"
	var (
		priv *rsa.PrivateKey
		jwks string
	)

	BeforeEach(func() {
		var err error
		priv, err = rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).NotTo(HaveOccurred())
		body, err := json.Marshal(jwtutil.BuildJWKS(jwtutil.BuildJWK(&priv.PublicKey, kid)))
		Expect(err).NotTo(HaveOccurred())
		jwks = string(body)
	})

	It("rejects an alg:none (unsigned) token", func() {
		token := unsignedToken(jwt.MapClaims{"iss": issuer, "sub": "u1"})
		_, err := jwtutil.VerifyOIDCToken(jwks, issuer, token)
		Expect(err).To(HaveOccurred())
	})

	It("rejects an RS256->HS256 algorithm-confusion forgery (public key as HMAC secret)", func() {
		// The classic attack: flip alg to HS256 and HMAC-sign with the server's PUBLIC key bytes.
		// If the verifier fed the public key to an HMAC verify, the forgery would pass.
		pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
		Expect(err).NotTo(HaveOccurred())
		pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"iss": issuer, "sub": "u1", "exp": time.Now().Add(time.Hour).Unix(),
		})
		tok.Header["kid"] = kid
		forged, err := tok.SignedString(pubPEM)
		Expect(err).NotTo(HaveOccurred())

		_, err = jwtutil.VerifyOIDCToken(jwks, issuer, forged)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a token whose kid is unknown to the JWKS", func() {
		token, err := jwtutil.SignRS256(jwt.MapClaims{
			"iss": issuer, "sub": "u1", "exp": time.Now().Add(time.Hour).Unix(),
		}, priv, "attacker-kid")
		Expect(err).NotTo(HaveOccurred())
		_, err = jwtutil.VerifyOIDCToken(jwks, issuer, token)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a token signed by a different (attacker) RSA key under a known kid", func() {
		evil, err := rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).NotTo(HaveOccurred())
		token, err := jwtutil.SignRS256(jwt.MapClaims{
			"iss": issuer, "sub": "u1", "exp": time.Now().Add(time.Hour).Unix(),
		}, evil, kid)
		Expect(err).NotTo(HaveOccurred())
		_, err = jwtutil.VerifyOIDCToken(jwks, issuer, token)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a token with a tampered payload (signature no longer matches)", func() {
		token, err := jwtutil.SignRS256(jwt.MapClaims{
			"iss": issuer, "sub": "u1", "exp": time.Now().Add(time.Hour).Unix(),
		}, priv, kid)
		Expect(err).NotTo(HaveOccurred())

		parts := strings.Split(token, ".")
		raw, err := base64.RawURLEncoding.DecodeString(parts[1])
		Expect(err).NotTo(HaveOccurred())
		var claims map[string]any
		Expect(json.Unmarshal(raw, &claims)).To(Succeed())
		claims["sub"] = "admin" // privilege escalation attempt
		nb, err := json.Marshal(claims)
		Expect(err).NotTo(HaveOccurred())
		parts[1] = base64.RawURLEncoding.EncodeToString(nb)

		_, err = jwtutil.VerifyOIDCToken(jwks, issuer, strings.Join(parts, "."))
		Expect(err).To(HaveOccurred())
	})
})

// AssertTokenUse blocks token substitution (RFC 9700 §4): an id token (meant for the client) must
// not be accepted where a resource-server access token is required.
var _ = Describe("AssertTokenUse", func() {
	It("accepts a token whose token_use matches", func() {
		Expect(jwtutil.AssertTokenUse(jwt.MapClaims{"token_use": jwtutil.TokenUseAccess}, jwtutil.TokenUseAccess)).To(Succeed())
	})

	It("rejects an id token where an access token is required", func() {
		Expect(jwtutil.AssertTokenUse(jwt.MapClaims{"token_use": jwtutil.TokenUseID}, jwtutil.TokenUseAccess)).To(HaveOccurred())
	})

	It("rejects a token with no token_use claim", func() {
		Expect(jwtutil.AssertTokenUse(jwt.MapClaims{"sub": "u1"}, jwtutil.TokenUseAccess)).To(HaveOccurred())
	})
})

var _ = Describe("Minter over a crypto.Signer", func() {
	It("mints an access token that verifies against the key's JWKS", func() {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).NotTo(HaveOccurred())
		kid := jwtutil.RSAThumbprint(&priv.PublicKey)
		minter := jwtutil.NewMinter("https://issuer.example", priv, kid)

		token, err := minter.AccessToken("u1", "c1", "openid email", "", jwtutil.Contact{Email: "u@example.com"})
		Expect(err).NotTo(HaveOccurred())

		jwks := jwtutil.BuildJWKS(jwtutil.BuildJWK(&priv.PublicKey, kid))
		jwksJSON, err := json.Marshal(jwks)
		Expect(err).NotTo(HaveOccurred())
		claims, err := jwtutil.VerifyOIDCToken(string(jwksJSON), "https://issuer.example", token)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims["email"]).To(Equal("u@example.com"))
	})
})

var _ = Describe("RSAThumbprint", func() {
	It("is deterministic for the same key and differs across keys", func() {
		a, _ := rsa.GenerateKey(rand.Reader, 2048)
		b, _ := rsa.GenerateKey(rand.Reader, 2048)
		Expect(jwtutil.RSAThumbprint(&a.PublicKey)).To(Equal(jwtutil.RSAThumbprint(&a.PublicKey)))
		Expect(jwtutil.RSAThumbprint(&a.PublicKey)).NotTo(Equal(jwtutil.RSAThumbprint(&b.PublicKey)))
	})
})
