// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Constants matching the reference implementation
const (
	MatterIDsByteLength = 8  // 8 bytes = 16 hex chars for Fabric ID, Root CA ID, etc.
	MatterIPKByteLength = 16 // 16 bytes = 32 hex chars for IPK
	CACertValidityYears = 15 // Root CA certificate validity period
)

// CAT ID ranges (matching reference implementation)
const (
	CATIDAdminLowerLimit   = 256  // 0x0100
	CATIDAdminUpperLimit   = 1023 // 0x03FF
	CATIDOperateLowerLimit = 1536 // 0x0600
	CATIDOperateUpperLimit = 2303 // 0x08FF
)

// FabricID length in hex characters (16 chars = 8 bytes)
const FabricIDHexLength = 16

// Matter Node ID upper limit
const MatterNodeIDUpperLimit = "FFFFFFEFFFFFFFFF"

// Matter Fabric Admin Vendor ID
const MatterFabricAdminVendorID = "131B" //Espressif's vendor ID

// Matter-specific X.509 OIDs from the Matter specification
var (
	MatterFabricIDOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 5}
	MatterRCACIDOID   = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 4}
	MatterNodeIDOID   = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 1}
	MatterCATIDOID    = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 6}
)

// GenerateCryptoRandToken generates random bytes and returns an uppercase hex string.
// This matches the reference implementation exactly.
func GenerateCryptoRandToken(numBytes int) (string, error) {
	bytes := make([]byte, numBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return strings.ToUpper(hex.EncodeToString(bytes)), nil
}

// GenerateFabricID generates a new Matter fabric ID (16 uppercase hex chars).
func GenerateFabricID() (string, error) {
	return GenerateCryptoRandToken(MatterIDsByteLength)
}

// GenerateIPK generates a new Matter IPK (32 uppercase hex chars).
func GenerateIPK() (string, error) {
	return GenerateCryptoRandToken(MatterIPKByteLength)
}

// MatterChallengeByteLength is the size of a Matter challenge (CSR nonce)
const MatterChallengeByteLength = 32

// GenerateMatterChallenge generates a 32-byte challenge for Matter device association.
// This challenge serves as the CSR nonce (64 uppercase hex chars).
func GenerateMatterChallenge() (string, error) {
	return GenerateCryptoRandToken(MatterChallengeByteLength)
}

// GenerateCATID generates a CAT ID for admin or operate roles.
// CAT IDs are 8 hex digit IDs where first 4 digits are identifiers
// and the remaining 4 digits are versions.
// Admin CAT IDs range from 0x0100 to 0x03FF.
// Operate CAT IDs range from 0x0600 to 0x08FF.
// Version starts from 0001.
func GenerateCATID(isAdmin bool) (string, error) {
	var lowerLimit, upperLimit int

	if isAdmin {
		lowerLimit = CATIDAdminLowerLimit
		upperLimit = CATIDAdminUpperLimit
	} else {
		lowerLimit = CATIDOperateLowerLimit
		upperLimit = CATIDOperateUpperLimit
	}

	rangeSize := big.NewInt(int64(upperLimit - lowerLimit + 1))
	n, err := rand.Int(rand.Reader, rangeSize)
	if err != nil {
		return "", fmt.Errorf("failed to generate CAT ID: %w", err)
	}

	catIDInt := int(n.Int64()) + lowerLimit
	return fmt.Sprintf("%04X0001", catIDInt), nil
}

// IncrementCATIDVersion increments the version portion of a CAT ID.
// CAT ID format: XXXXVVVV (4 hex identifier + 4 hex version)
// Example: "01FF0001" → "01FF0002"
func IncrementCATIDVersion(catID string) (string, error) {
	if len(catID) != 8 {
		return "", fmt.Errorf("invalid CAT ID length: expected 8, got %d", len(catID))
	}

	identifier := catID[:4]
	versionStr := catID[4:]

	version, err := strconv.ParseUint(versionStr, 16, 16)
	if err != nil {
		return "", fmt.Errorf("invalid version: %w", err)
	}

	newVersion := version + 1
	return fmt.Sprintf("%s%04X", identifier, newVersion), nil
}

const matterUserNodeIDDomain = "rmng-matter-user-node-id-v1"

// DeriveMatterUserNodeID derives a stable, fabric-scoped Matter Node ID for
// one user's controller key. Different operational keys (for example, keys held by
// two phones) produce different Node IDs without requiring per-controller storage.
func DeriveMatterUserNodeID(fabricID, rmngUserID string, publicKey *ecdsa.PublicKey) (string, error) {
	if rmngUserID == "" {
		return "", errors.New("empty rmng user ID")
	}
	if _, err := ECDSAP256PublicKey(publicKey); err != nil {
		return "", fmt.Errorf("controller public key is unusable: %w", err)
	}

	fabricBytes, err := hex.DecodeString(fabricID)
	if err != nil || len(fabricBytes) != MatterIDsByteLength {
		return "", fmt.Errorf("invalid Matter fabric ID %q", fabricID)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal controller public key: %w", err)
	}

	h := sha256.New()
	for _, field := range [][]byte{
		[]byte(matterUserNodeIDDomain),
		fabricBytes,
		[]byte(rmngUserID),
		publicKeyDER,
	} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		_, _ = h.Write(length[:])
		_, _ = h.Write(field)
	}

	upperLimit := new(big.Int)
	if _, ok := upperLimit.SetString(MatterNodeIDUpperLimit, 16); !ok {
		return "", errors.New("invalid Matter Node ID upper limit")
	}
	nodeID := new(big.Int).SetBytes(h.Sum(nil))
	nodeID.Mod(nodeID, upperLimit)
	nodeID.Add(nodeID, big.NewInt(1))
	return fmt.Sprintf("%016X", nodeID), nil
}

// CreateECKeyPair generates an ECDSA P-256 key pair.
func CreateECKeyPair() (*ecdsa.PrivateKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ECDSA key pair: %w", err)
	}
	return privateKey, nil
}

// PrivateKeyToPEM converts an ECDSA private key to PKCS8 PEM format.
func PrivateKeyToPEM(key *ecdsa.PrivateKey) (string, error) {
	privBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	privKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	})
	if privKeyPEM == nil {
		return "", errors.New("failed to encode private key to PEM")
	}

	return string(privKeyPEM), nil
}

// ParsePrivateKeyPEM parses a PEM-encoded PKCS8 private key.
func ParsePrivateKeyPEM(pemStr string) (*ecdsa.PrivateKey, error) {
	if pemStr == "" {
		return nil, errors.New("empty private key PEM string")
	}

	pemBlock, _ := pem.Decode([]byte(pemStr))
	if pemBlock == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	privKeyInterface, err := x509.ParsePKCS8PrivateKey(pemBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	privKey, ok := privKeyInterface.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not ECDSA")
	}

	return privKey, nil
}

// ECDSAP256PublicKey narrows an untyped public key to ECDSA P-256, the only curve Matter certificates permit. Gate every key that reaches certificate generation on this: crypto/ecdh supports P-256/384/521 only, so an unchecked P-224 key nils out inside GenerateSubjectKeyID.
func ECDSAP256PublicKey(pubKey any) (*ecdsa.PublicKey, error) {
	key, ok := pubKey.(*ecdsa.PublicKey)
	if !ok || key == nil {
		return nil, errors.New("public key is not ECDSA")
	}
	if key.Curve == nil {
		return nil, errors.New("public key has no curve")
	}
	if name := key.Curve.Params().Name; name != elliptic.P256().Params().Name {
		return nil, fmt.Errorf("public key curve %q is not P-256", name)
	}
	return key, nil
}

// GenerateSubjectKeyID creates the subject key identifier from the public key.
// It uses SHA1 hash of the raw public key bytes as per the Matter specification.
func GenerateSubjectKeyID(pubKey *ecdsa.PublicKey) ([]byte, error) {
	if pubKey == nil {
		return nil, errors.New("nil public key")
	}
	ecdhKey, err := pubKey.ECDH()
	if err != nil {
		return nil, fmt.Errorf("failed to convert public key for subject key ID: %w", err)
	}
	hashed := sha1.Sum(ecdhKey.Bytes())
	return hashed[:], nil
}

// ConvertToMatterUTF8 converts a string to ASN.1 UTF8String for certificate subject fields.
func ConvertToMatterUTF8(value string) (asn1.RawValue, error) {
	utf8Bytes, err := asn1.MarshalWithParams(value, "utf8")
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("failed to marshal to UTF8: %w", err)
	}
	return asn1.RawValue{FullBytes: utf8Bytes}, nil
}

// GenerateCertSerialNumber generates a random serial number for certificates.
func GenerateCertSerialNumber() (*big.Int, error) {
	serialNumber := make([]byte, 8)
	if _, err := rand.Read(serialNumber); err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}
	return new(big.Int).SetBytes(serialNumber), nil
}

// MatterFabricCertificate holds the Root CA certificate and its private key
type MatterFabricCertificate struct {
	CertificatePEM string
	PrivateKeyPEM  string
	CertTemplate   *x509.Certificate
}

// CreateRootCACertificate creates a self-signed Root CA certificate for a Matter fabric.
// The certificate is created according to Matter specification with proper OIDs.
func CreateRootCACertificate(fabricID string) (*MatterFabricCertificate, error) {
	return CreateRootCACertificateWithKey(fabricID, nil, "")
}

// CreateRootCACertificateWithKey creates a self-signed Root CA certificate using a provided private key.
// If privateKey is nil, a new key pair is generated. If rootCAID is empty, a new one is generated.
// This function is useful for testing to ensure deterministic certificate generation.
func CreateRootCACertificateWithKey(fabricID string, privateKey *ecdsa.PrivateKey, rootCAID string) (*MatterFabricCertificate, error) {
	var err error

	// Convert Fabric ID to UTF8 for certificate subject
	fabricIDUTF8, err := ConvertToMatterUTF8(fabricID)
	if err != nil {
		return nil, fmt.Errorf("failed to convert fabric ID to UTF8: %w", err)
	}

	// Generate Root CA ID if not provided
	if rootCAID == "" {
		rootCAID, err = GenerateCryptoRandToken(MatterIDsByteLength)
		if err != nil {
			return nil, fmt.Errorf("failed to generate Root CA ID: %w", err)
		}
	}

	// Convert Root CA ID to UTF8
	rootCAIDUTF8, err := ConvertToMatterUTF8(rootCAID)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Root CA ID to UTF8: %w", err)
	}

	// Build certificate subject with Matter-specific OIDs
	subjectExtraNames := []pkix.AttributeTypeAndValue{
		{Type: MatterFabricIDOID, Value: fabricIDUTF8},
		{Type: MatterRCACIDOID, Value: rootCAIDUTF8},
	}

	// Generate serial number
	serialNumber, err := GenerateCertSerialNumber()
	if err != nil {
		return nil, err
	}

	// Generate EC private key for CA if not provided
	if privateKey == nil {
		privateKey, err = CreateECKeyPair()
		if err != nil {
			return nil, err
		}
	}

	// Create certificate template
	now := time.Now()
	caCertTemplate := &x509.Certificate{
		SerialNumber:          serialNumber,
		IsCA:                  true,
		NotBefore:             now,
		NotAfter:              now.AddDate(CACertValidityYears, 0, 0),
		Subject:               pkix.Name{ExtraNames: subjectExtraNames},
		KeyUsage:              x509.KeyUsageCRLSign | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// Set Subject Key ID and Authority Key ID (self-signed)
	subjectKeyID, err := GenerateSubjectKeyID(&privateKey.PublicKey)
	if err != nil {
		return nil, err
	}
	caCertTemplate.SubjectKeyId = subjectKeyID
	caCertTemplate.AuthorityKeyId = subjectKeyID

	// Create self-signed certificate
	caCertBytes, err := x509.CreateCertificate(
		rand.Reader,
		caCertTemplate,
		caCertTemplate,
		&privateKey.PublicKey,
		privateKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Root CA certificate: %w", err)
	}

	// Encode certificate to PEM
	caCertPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertBytes,
	})
	if caCertPEM == nil {
		return nil, errors.New("failed to encode Root CA certificate to PEM")
	}

	// Convert private key to PEM
	privateKeyPEM, err := PrivateKeyToPEM(privateKey)
	if err != nil {
		return nil, err
	}

	return &MatterFabricCertificate{
		CertificatePEM: string(caCertPEM),
		PrivateKeyPEM:  privateKeyPEM,
		CertTemplate:   caCertTemplate,
	}, nil
}

// ParseCertificatePEM parses a PEM-encoded X.509 certificate.
func ParseCertificatePEM(pemStr string) (*x509.Certificate, error) {
	pemBlock, _ := pem.Decode([]byte(pemStr))
	if pemBlock == nil {
		return nil, errors.New("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(pemBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return cert, nil
}

// NOC (Node Operational Certificate) generation constants
const (
	// NOCValidityYears is the validity period for NOC certificates
	NOCValidityYears = 10
)

// Extended Key Usage OIDs for Matter certificates
var (
	ExtKeyUsageOID           = asn1.ObjectIdentifier{2, 5, 29, 37}
	ExtKeyUsageServerAuthOID = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 1}
	ExtKeyUsageClientAuthOID = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 2}
)

// CreateExtKeyUsageExtension creates the ExtKeyUsage extension for Matter NOCs
// that includes both ServerAuth and ClientAuth.
func CreateExtKeyUsageExtension() pkix.Extension {
	extKeyUsage := pkix.Extension{
		Id:       ExtKeyUsageOID,
		Critical: true,
	}
	extKeyUsage.Value, _ = asn1.Marshal([]asn1.ObjectIdentifier{
		ExtKeyUsageServerAuthOID,
		ExtKeyUsageClientAuthOID,
	})
	return extKeyUsage
}

// ParseCSR parses a PEM-encoded Certificate Signing Request.
// Returns the parsed CSR or an error if parsing fails.
func ParseCSR(csrPEM string) (*x509.CertificateRequest, error) {
	pemBlock, _ := pem.Decode([]byte(csrPEM))
	if pemBlock == nil {
		return nil, errors.New("failed to decode CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(pemBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSR: %w", err)
	}

	// Verify the CSR signature
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("invalid CSR signature: %w", err)
	}

	return csr, nil
}

// NOCInput contains the parameters needed to create a Node Operational Certificate
type NOCInput struct {
	// CSR is the parsed Certificate Signing Request
	CSR *x509.CertificateRequest
	// FabricID is the Matter fabric identifier
	FabricID string
	// MatterNodeID is the Matter node identifier for this certificate
	MatterNodeID string
	// GroupCATIDs
	GroupCATID string
	// RootCACert is the Root CA certificate
	RootCACert *x509.Certificate
	// RootCAPrivateKey is the Root CA private key for signing
	RootCAPrivateKey *ecdsa.PrivateKey
}

// CreateNOC creates a Node Operational Certificate from the input parameters.
// The NOC is signed by the Root CA and includes Matter-specific OIDs.
func CreateNOC(input *NOCInput) (string, error) {
	// Convert FabricID to UTF8 for certificate subject
	fabricIDUTF8, err := ConvertToMatterUTF8(input.FabricID)
	if err != nil {
		return "", fmt.Errorf("failed to convert fabric ID to UTF8: %w", err)
	}

	// Convert MatterNodeID to UTF8
	nodeIDUTF8, err := ConvertToMatterUTF8(input.MatterNodeID)
	if err != nil {
		return "", fmt.Errorf("failed to convert node ID to UTF8: %w", err)
	}

	// Build subject extra names with Matter-specific OIDs
	subjectExtraNames := []pkix.AttributeTypeAndValue{
		{Type: MatterFabricIDOID, Value: fabricIDUTF8},
		{Type: MatterNodeIDOID, Value: nodeIDUTF8},
	}

	if input.GroupCATID != "" {
		catIDUTF8, err := ConvertToMatterUTF8(input.GroupCATID)
		if err != nil {
			return "", fmt.Errorf("failed to convert CAT ID to UTF8: %w", err)
		}
		subjectExtraNames = append(subjectExtraNames, pkix.AttributeTypeAndValue{
			Type:  MatterCATIDOID,
			Value: catIDUTF8,
		})
	}

	// Generate serial number
	serialNumber, err := GenerateCertSerialNumber()
	if err != nil {
		return "", err
	}

	// Create NOC certificate template
	now := time.Now()
	notAfter := now.AddDate(NOCValidityYears, 0, 0)

	// Ensure NOC validity doesn't exceed CA validity
	if notAfter.After(input.RootCACert.NotAfter) {
		notAfter = input.RootCACert.NotAfter
	}

	csrPubKey, err := ECDSAP256PublicKey(input.CSR.PublicKey)
	if err != nil {
		return "", fmt.Errorf("invalid CSR public key: %w", err)
	}

	subjectKeyID, err := GenerateSubjectKeyID(csrPubKey)
	if err != nil {
		return "", err
	}

	nocTemplate := &x509.Certificate{
		Signature:             input.CSR.Signature,
		SignatureAlgorithm:    input.CSR.SignatureAlgorithm,
		PublicKeyAlgorithm:    input.CSR.PublicKeyAlgorithm,
		PublicKey:             input.CSR.PublicKey,
		SerialNumber:          serialNumber,
		Issuer:                input.RootCACert.Subject,
		Subject:               pkix.Name{ExtraNames: subjectExtraNames},
		NotBefore:             now,
		NotAfter:              notAfter,
		IsCA:                  false,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		ExtraExtensions:       []pkix.Extension{CreateExtKeyUsageExtension()},
		SubjectKeyId:          subjectKeyID,
		BasicConstraintsValid: true,
	}

	// Sign the NOC with the Root CA private key
	nocBytes, err := x509.CreateCertificate(
		rand.Reader,
		nocTemplate,
		input.RootCACert,
		input.CSR.PublicKey,
		input.RootCAPrivateKey,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create NOC certificate: %w", err)
	}

	// Encode to PEM
	nocPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: nocBytes,
	})
	if nocPEM == nil {
		return "", errors.New("failed to encode NOC to PEM")
	}

	return string(nocPEM), nil
}

// JSON key constants for MatterCapabilityData fields.
// These match the json tags on the MatterCapabilityData struct.
const (
	KeyFabricID          = "fabric_id"
	KeyRootCA            = "root_ca"
	KeyRootCAPrivKey     = "root_ca_priv_key"
	KeyIPK               = "ipk"
	KeyGroupCATIDAdmin   = "group_cat_id_admin"
	KeyGroupCATIDOperate = "group_cat_id_operate"
)

// ConvertRawSignatureToDER converts a 64-byte raw r||s ECDSA signature to DER format.
// Matter devices produce signatures in raw format (32-byte r concatenated with 32-byte s),
// but Go's ecdsa.VerifyASN1 expects DER-encoded signatures.
func ConvertRawSignatureToDER(rawSig []byte) ([]byte, error) {
	if len(rawSig) != 64 {
		return nil, fmt.Errorf("raw signature must be 64 bytes, got %d", len(rawSig))
	}

	r := new(big.Int).SetBytes(rawSig[:32])
	s := new(big.Int).SetBytes(rawSig[32:])

	// DER encoding:
	// SEQUENCE {
	//   INTEGER r
	//   INTEGER s
	// }
	type ecdsaSignature struct {
		R, S *big.Int
	}

	derSig, err := asn1.Marshal(ecdsaSignature{R: r, S: s})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal signature to DER: %w", err)
	}

	return derSig, nil
}

// VerifyECDSASignatureRaw verifies an ECDSA signature in raw r||s format against a hash.
// This is a convenience function that converts the raw signature to DER and verifies.
func VerifyECDSASignatureRaw(pubKey *ecdsa.PublicKey, hash, rawSig []byte) error {
	derSig, err := ConvertRawSignatureToDER(rawSig)
	if err != nil {
		return err
	}

	if !ecdsa.VerifyASN1(pubKey, hash, derSig) {
		return errors.New("ECDSA signature verification failed")
	}

	return nil
}
