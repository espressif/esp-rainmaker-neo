// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kms_types "github.com/aws/aws-sdk-go-v2/service/kms/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MockKMS", func() {
	const keyID = "arn:aws:kms:us-east-1:111122223333:key/test-key"

	var (
		m      *mock.MockKMS
		key    *ecdsa.PrivateKey
		ctx    context.Context
		digest []byte
	)

	BeforeEach(func() {
		m = mock.NewMockKMS()
		key = m.AddKey(keyID)
		ctx = context.Background()
		d := sha256.Sum256([]byte("some certificate TBS bytes"))
		digest = d[:]
	})

	Describe("AddKey", func() {
		It("returns a usable P-256 key", func() {
			Expect(key).NotTo(BeNil())
			Expect(key.Curve.Params().BitSize).To(Equal(256))
		})

		It("gives each key ID an independent key", func() {
			other := m.AddKey("other-key")
			Expect(other.Equal(key)).To(BeFalse())
		})
	})

	Describe("GetPublicKey", func() {
		It("returns the added key's public half with the P-256 SIGN_VERIFY profile", func() {
			out, err := m.GetPublicKey(ctx, &kms.GetPublicKeyInput{KeyId: aws.String(keyID)})
			Expect(err).To(BeNil())
			Expect(aws.ToString(out.KeyId)).To(Equal(keyID))
			Expect(out.KeySpec).To(Equal(kms_types.KeySpecEccNistP256))
			Expect(out.KeyUsage).To(Equal(kms_types.KeyUsageTypeSignVerify))
			Expect(out.SigningAlgorithms).To(Equal([]kms_types.SigningAlgorithmSpec{
				kms_types.SigningAlgorithmSpecEcdsaSha256,
			}))

			// The returned DER must parse back to exactly the key AddKey handed out.
			parsed, err := x509.ParsePKIXPublicKey(out.PublicKey)
			Expect(err).To(BeNil())
			pub, ok := parsed.(*ecdsa.PublicKey)
			Expect(ok).To(BeTrue())
			Expect(pub.Equal(&key.PublicKey)).To(BeTrue())
		})

		It("returns a NotFoundException for an unknown key ID", func() {
			_, err := m.GetPublicKey(ctx, &kms.GetPublicKeyInput{KeyId: aws.String("no-such-key")})
			Expect(err).To(HaveOccurred())
			var nf *kms_types.NotFoundException
			Expect(errors.As(err, &nf)).To(BeTrue())
		})

		It("requires a KeyId", func() {
			_, err := m.GetPublicKey(ctx, &kms.GetPublicKeyInput{})
			Expect(err).To(HaveOccurred())

			_, err = m.GetPublicKey(ctx, nil)
			Expect(err).To(HaveOccurred())
		})

		It("returns the injected error when GetPublicKeyError is set", func() {
			m.GetPublicKeyError = errors.New("kms unavailable")
			_, err := m.GetPublicKey(ctx, &kms.GetPublicKeyInput{KeyId: aws.String(keyID)})
			Expect(err).To(MatchError("kms unavailable"))
		})
	})

	Describe("Sign", func() {
		validInput := func() *kms.SignInput {
			return &kms.SignInput{
				KeyId:            aws.String(keyID),
				Message:          digest,
				MessageType:      kms_types.MessageTypeDigest,
				SigningAlgorithm: kms_types.SigningAlgorithmSpecEcdsaSha256,
			}
		}

		It("produces a real signature that verifies against the key", func() {
			out, err := m.Sign(ctx, validInput())
			Expect(err).To(BeNil())
			Expect(aws.ToString(out.KeyId)).To(Equal(keyID))
			Expect(out.SigningAlgorithm).To(Equal(kms_types.SigningAlgorithmSpecEcdsaSha256))
			Expect(ecdsa.VerifyASN1(&key.PublicKey, digest, out.Signature)).To(BeTrue())
		})

		It("records each call so tests can assert the signing parameters", func() {
			_, err := m.Sign(ctx, validInput())
			Expect(err).To(BeNil())
			_, err = m.Sign(ctx, validInput())
			Expect(err).To(BeNil())

			Expect(m.SignCalls).To(HaveLen(2))
			Expect(m.SignCalls[0].MessageType).To(Equal(kms_types.MessageTypeDigest))
			Expect(m.SignCalls[0].SigningAlgorithm).To(Equal(kms_types.SigningAlgorithmSpecEcdsaSha256))
			Expect(m.SignCalls[0].Message).To(Equal(digest))
		})

		It("rejects a message type other than DIGEST", func() {
			in := validInput()
			in.MessageType = kms_types.MessageTypeRaw
			_, err := m.Sign(ctx, in)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("MessageType DIGEST"))
		})

		It("rejects an unsupported signing algorithm", func() {
			in := validInput()
			in.SigningAlgorithm = kms_types.SigningAlgorithmSpecEcdsaSha384
			_, err := m.Sign(ctx, in)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported signing algorithm"))
		})

		It("rejects a digest that is not 32 bytes", func() {
			in := validInput()
			in.Message = []byte("too short")
			_, err := m.Sign(ctx, in)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("32 bytes"))
		})

		It("returns a NotFoundException for an unknown key ID", func() {
			in := validInput()
			in.KeyId = aws.String("no-such-key")
			_, err := m.Sign(ctx, in)
			Expect(err).To(HaveOccurred())
			var nf *kms_types.NotFoundException
			Expect(errors.As(err, &nf)).To(BeTrue())
		})

		It("requires a KeyId", func() {
			_, err := m.Sign(ctx, &kms.SignInput{})
			Expect(err).To(HaveOccurred())

			_, err = m.Sign(ctx, nil)
			Expect(err).To(HaveOccurred())
		})

		It("returns the injected error when SignError is set, and records nothing", func() {
			m.SignError = errors.New("kms throttled")
			_, err := m.Sign(ctx, validInput())
			Expect(err).To(MatchError("kms throttled"))
			Expect(m.SignCalls).To(BeEmpty())
		})
	})

	// The mock exists so an issued certificate can actually be verified. Prove
	// the two calls compose: a signature made with Sign verifies against the
	// public key handed out by GetPublicKey.
	Describe("GetPublicKey + Sign together", func() {
		It("signs with a key whose public half GetPublicKey reports", func() {
			pubOut, err := m.GetPublicKey(ctx, &kms.GetPublicKeyInput{KeyId: aws.String(keyID)})
			Expect(err).To(BeNil())
			parsed, err := x509.ParsePKIXPublicKey(pubOut.PublicKey)
			Expect(err).To(BeNil())
			pub := parsed.(*ecdsa.PublicKey)

			signOut, err := m.Sign(ctx, &kms.SignInput{
				KeyId:            aws.String(keyID),
				Message:          digest,
				MessageType:      kms_types.MessageTypeDigest,
				SigningAlgorithm: kms_types.SigningAlgorithmSpecEcdsaSha256,
			})
			Expect(err).To(BeNil())
			Expect(ecdsa.VerifyASN1(pub, digest, signOut.Signature)).To(BeTrue())
		})
	})
})
