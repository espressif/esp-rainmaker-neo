// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kms_types "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// MockKMS emulates the two KMS calls the signers use, backed by a real in-memory key
// per key ID: P-256 for certificates, RSA for tokens.
//
// It signs for real rather than returning a canned blob: the issuer's output is an
// X.509 certificate, and a certificate signed with a fake signature cannot be
// verified. Real signing lets tests assert what actually matters — that the issued
// chain verifies — instead of only that some bytes were produced.
type MockKMS struct {
	mutex   sync.RWMutex
	keys    map[string]*ecdsa.PrivateKey
	rsaKeys map[string]*rsa.PrivateKey

	// Call capture, so tests can assert the signing parameters rather than
	// trusting that the right algorithm was requested.
	SignCalls []kms.SignInput

	SignError         error
	GetPublicKeyError error
}

var mockKMS *MockKMS

func NewMockKMS() *MockKMS {
	mockKMS = &MockKMS{
		keys:    make(map[string]*ecdsa.PrivateKey),
		rsaKeys: make(map[string]*rsa.PrivateKey),
	}
	return mockKMS
}

// NewMockRSAKMS returns a mock holding one freshly generated RSA key under keyID,
// alongside the key itself so a caller can build the JWKS this mock will sign against.
func NewMockRSAKMS(keyID string) (*MockKMS, *rsa.PrivateKey) {
	m := NewMockKMS()
	return m, m.AddRSAKeyWith(keyID, nil)
}

// AddRSAKeyWith registers an RSA key under keyID, generating one when key is nil. A
// caller that passes its own key can verify signatures against a JWKS it already holds.
func (m *MockKMS) AddRSAKeyWith(keyID string, key *rsa.PrivateKey) *rsa.PrivateKey {
	if key == nil {
		generated, err := rsa.GenerateKey(crand.Reader, 2048)
		if err != nil {
			panic("mock kms: failed to generate RSA key: " + err.Error())
		}
		key = generated
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.rsaKeys[keyID] = key
	return key
}

// AddKey creates an ECDSA P-256 key under keyID and returns it, so a test can
// independently verify signatures the mock produced.
func (m *MockKMS) AddKey(keyID string) *ecdsa.PrivateKey {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	key, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		panic("mock kms: failed to generate key: " + err.Error())
	}
	m.keys[keyID] = key
	return key
}

func (m *MockKMS) GetPublicKey(ctx context.Context, params *kms.GetPublicKeyInput, optFns ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error) {
	if m.GetPublicKeyError != nil {
		return nil, m.GetPublicKeyError
	}
	if params == nil || params.KeyId == nil {
		return nil, errors.New("mock kms: KeyId is required")
	}

	pub, spec, algorithm, err := m.publicKey(*params.KeyId)
	if err != nil {
		return nil, err
	}

	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}

	return &kms.GetPublicKeyOutput{
		KeyId:             params.KeyId,
		PublicKey:         der,
		KeySpec:           spec,
		KeyUsage:          kms_types.KeyUsageTypeSignVerify,
		SigningAlgorithms: []kms_types.SigningAlgorithmSpec{algorithm},
	}, nil
}

// publicKey resolves keyID to its public half plus the spec and algorithm that key
// supports, so GetPublicKey and Sign agree on what a given key can do.
func (m *MockKMS) publicKey(keyID string) (crypto.PublicKey, kms_types.KeySpec, kms_types.SigningAlgorithmSpec, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if key, ok := m.rsaKeys[keyID]; ok {
		return &key.PublicKey, kms_types.KeySpecRsa2048, kms_types.SigningAlgorithmSpecRsassaPkcs1V15Sha256, nil
	}
	if key, ok := m.keys[keyID]; ok {
		return &key.PublicKey, kms_types.KeySpecEccNistP256, kms_types.SigningAlgorithmSpecEcdsaSha256, nil
	}
	return nil, "", "", &kms_types.NotFoundException{Message: aws.String("key not found: " + keyID)}
}

func (m *MockKMS) Sign(ctx context.Context, params *kms.SignInput, optFns ...func(*kms.Options)) (*kms.SignOutput, error) {
	if m.SignError != nil {
		return nil, m.SignError
	}
	if params == nil || params.KeyId == nil {
		return nil, errors.New("mock kms: KeyId is required")
	}

	m.mutex.Lock()
	m.SignCalls = append(m.SignCalls, *params)
	m.mutex.Unlock()

	_, _, algorithm, err := m.publicKey(*params.KeyId)
	if err != nil {
		return nil, err
	}

	// Mirror the real service: a pre-computed digest requires MessageType DIGEST, each
	// key spec supports exactly one SHA-256 algorithm, and the digest is 32 bytes.
	if params.MessageType != kms_types.MessageTypeDigest {
		return nil, errors.New("mock kms: expected MessageType DIGEST, got " + string(params.MessageType))
	}
	if params.SigningAlgorithm != algorithm {
		return nil, errors.New("mock kms: unsupported signing algorithm " + string(params.SigningAlgorithm))
	}
	if len(params.Message) != crypto.SHA256.Size() {
		return nil, errors.New("mock kms: SHA-256 digest must be 32 bytes")
	}

	sig, err := m.sign(*params.KeyId, params.Message)
	if err != nil {
		return nil, err
	}

	return &kms.SignOutput{
		KeyId:            params.KeyId,
		Signature:        sig,
		SigningAlgorithm: params.SigningAlgorithm,
	}, nil
}

// sign produces a real signature with whichever key kind keyID names. ecdsa.SignASN1
// and rsa.SignPKCS1v15 each return the encoding the real service returns, which is also
// what crypto/x509 and jwtutils expect back from a crypto.Signer.
func (m *MockKMS) sign(keyID string, digest []byte) ([]byte, error) {
	m.mutex.RLock()
	rsaKey, isRSA := m.rsaKeys[keyID]
	ecKey := m.keys[keyID]
	m.mutex.RUnlock()

	if isRSA {
		return rsa.SignPKCS1v15(crand.Reader, rsaKey, crypto.SHA256, digest)
	}
	return ecdsa.SignASN1(crand.Reader, ecKey, digest)
}
