// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package kmsutil adapts an AWS KMS asymmetric key to crypto.Signer, so the private key
// never exists in this process. See docs/en/specs/assisted-claiming.md.
package kmsutil

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kms_types "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

var publicKeyCache sync.Map

type Signer struct {
	ctx       context.Context
	keyID     string
	pub       crypto.PublicKey
	algorithm kms_types.SigningAlgorithmSpec
}

func NewSigner(ctx context.Context, keyID string) (*Signer, error) {
	return newSigner(ctx, keyID, kms_types.KeySpecEccNistP256, kms_types.SigningAlgorithmSpecEcdsaSha256)
}

// NewRSASigner needs only kms:Sign: minting reads no public half (the JWKS is published
// from RSAPublicKey by publish_discovery), so the key spec is enforced by KMS itself at
// Sign, keeping kms:GetPublicKey off every token-minting lambda.
func NewRSASigner(ctx context.Context, keyID string) (*Signer, error) {
	if keyID == "" {
		return nil, rmerror.NewRMError(nil, "kms key id is empty")
	}
	return &Signer{ctx: ctx, keyID: keyID, algorithm: kms_types.SigningAlgorithmSpecRsassaPkcs1V15Sha256}, nil
}

func newSigner(ctx context.Context, keyID string, want kms_types.KeySpec, algorithm kms_types.SigningAlgorithmSpec) (*Signer, error) {
	if keyID == "" {
		return nil, rmerror.NewRMError(nil, "kms key id is empty")
	}

	if cached, ok := publicKeyCache.Load(keyID); ok {
		return &Signer{ctx: ctx, keyID: keyID, pub: cached, algorithm: algorithm}, nil
	}

	pub, err := publicKey(ctx, keyID, want)
	if err != nil {
		return nil, err
	}

	publicKeyCache.Store(keyID, pub)
	return &Signer{ctx: ctx, keyID: keyID, pub: pub, algorithm: algorithm}, nil
}

func publicKey(ctx context.Context, keyID string, want kms_types.KeySpec) (crypto.PublicKey, error) {
	out, err := awscommon.GetKMSClient().GetPublicKey(ctx, &kms.GetPublicKeyInput{
		KeyId: aws.String(keyID),
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to fetch KMS public key")
	}
	if out.KeySpec != want {
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("KMS key %s has spec %s, expected %s", keyID, out.KeySpec, want))
	}

	parsed, err := x509.ParsePKIXPublicKey(out.PublicKey)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse KMS public key")
	}
	return parsed, nil
}

func (s *Signer) Public() crypto.PublicKey {
	return s.pub
}

func (s *Signer) KeyID() string {
	return s.keyID
}

func (s *Signer) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if opts == nil || opts.HashFunc() != crypto.SHA256 {
		return nil, rmerror.NewRMError(nil, "KMS signer supports SHA-256 only")
	}
	if len(digest) != crypto.SHA256.Size() {
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("expected a %d-byte digest, got %d", crypto.SHA256.Size(), len(digest)))
	}

	out, err := awscommon.GetKMSClient().Sign(s.ctx, &kms.SignInput{
		KeyId:            aws.String(s.keyID),
		Message:          digest,
		MessageType:      kms_types.MessageTypeDigest,
		SigningAlgorithm: s.algorithm,
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "kms sign failed")
	}

	return out.Signature, nil
}

func RSAPublicKey(ctx context.Context, keyID string) (*rsa.PublicKey, error) {
	pub, err := publicKey(ctx, keyID, kms_types.KeySpecRsa2048)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, rmerror.NewRMError(nil, "KMS public key is not RSA")
	}
	return rsaPub, nil
}

func ResetPublicKeyCache() {
	publicKeyCache.Range(func(k, _ any) bool {
		publicKeyCache.Delete(k)
		return true
	})
}

var _ crypto.Signer = (*Signer)(nil)
