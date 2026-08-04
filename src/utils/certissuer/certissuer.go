// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package certissuer produces device certificates from a validated public key
under a named profile.

Two properties are structural rather than checked:

  - The subject is built here from server-held values. Nothing in a Profile
    comes from a CSR, so a caller cannot influence the identity it receives and
    there is no Common Name to validate downstream.
  - The signing key is a crypto.Signer. In production that is a KMS-backed
    signer, so no code path in this package can materialize private key
    material.

Certificate profiles are expressed as data (Profile) rather than as boolean
parameters on a shared build function. The Matter attestation profile is rigid
enough that flag-driven construction drifts out of conformance; keeping the
differences declarative means a new profile cannot silently inherit the wrong
extension set.

See misc/specs/assisted-claiming.md.
*/
package certissuer

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"math/big"
	"time"
)

const (
	// DeviceCertValidity is deliberately long. There is no certificate renewal
	// path — a device needing a new certificate re-claims — so expiry is not a
	// control here. Revocation is effected by deactivating the certificate in
	// AWS IoT, which is why replaced certificates are deactivated and never
	// deleted (a deleted certificate's identity is free to be registered
	// again). See requirements Req 8.8.
	DeviceCertValidity = 100 * 365 * 24 * time.Hour

	// serialBits is 128, so the DER-encoded serial stays well inside the
	// 20-octet ceiling the Matter attestation profile imposes (Req 8.7).
	serialBits = 128

	// clockSkew backdates NotBefore so a device whose clock is slightly behind
	// the signer does not reject its own freshly issued certificate.
	clockSkew = 5 * time.Minute
)

// Profile is the complete description of a certificate to be produced. Every
// field is server-determined.
type Profile struct {
	// CAID names the issuing CA. Recorded alongside the node so a second CA
	// can be introduced and the first retired without ambiguity (Req 11.7).
	CAID string

	// CommonName is the node ID. It becomes the certificate CN, and the node
	// ID is simultaneously the IoT Thing name, the MQTT client ID, and the
	// Matter operational Node ID (Req 10.1).
	CommonName string

	// ExtraNames carries subject attributes beyond the CN. Empty for the
	// device profile; the Matter profile supplies the Vendor ID and Product ID
	// attributes here.
	ExtraNames []pkix.AttributeTypeAndValue

	// Validity defaults to DeviceCertValidity when zero.
	Validity time.Duration

	// Subject carries operator-configured organizational attributes (country,
	// organization, etc.) added to the subject alongside the CN. All optional.
	Subject Subject
}

// Subject carries operator-configurable organizational attributes for a
// certificate subject, applied in addition to the CN. All fields are optional;
// empty ones are omitted. Email is emitted as an emailAddress RDN because
// pkix.Name has no dedicated field for it.
type Subject struct {
	Country            string `json:"country,omitempty"`
	Province           string `json:"state,omitempty"`
	Locality           string `json:"locality,omitempty"`
	Organization       string `json:"organization,omitempty"`
	OrganizationalUnit string `json:"organizational_unit,omitempty"`
	Email              string `json:"email,omitempty"`
}

// oidEmailAddress is the PKCS#9 emailAddress attribute (1.2.840.113549.1.9.1).
var oidEmailAddress = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}

// pkixName builds a pkix.Name for commonName, adding this subject's
// organizational attributes (and an emailAddress RDN when set).
func (s Subject) pkixName(commonName string) pkix.Name {
	n := pkix.Name{CommonName: commonName}
	if s.Country != "" {
		n.Country = []string{s.Country}
	}
	if s.Province != "" {
		n.Province = []string{s.Province}
	}
	if s.Locality != "" {
		n.Locality = []string{s.Locality}
	}
	if s.Organization != "" {
		n.Organization = []string{s.Organization}
	}
	if s.OrganizationalUnit != "" {
		n.OrganizationalUnit = []string{s.OrganizationalUnit}
	}
	if s.Email != "" {
		n.ExtraNames = append(n.ExtraNames, pkix.AttributeTypeAndValue{
			Type:  oidEmailAddress,
			Value: s.Email,
		})
	}
	return n
}

// Result is an issued certificate and the chain needed to verify it.
type Result struct {
	// CertPEM is the leaf certificate, PEM encoded.
	CertPEM string
	// ChainPEM is the issuing CA certificate, PEM encoded.
	ChainPEM string
	// CAID echoes the profile's CA identifier for recording on the node.
	CAID string
}

// Issuer produces a certificate for pub under the given profile.
type Issuer interface {
	Issue(ctx context.Context, pub crypto.PublicKey, p Profile) (*Result, error)
}

// SigningIssuer signs with any crypto.Signer against a fixed CA certificate.
// Production wires a KMS-backed signer; tests can supply a local key.
type SigningIssuer struct {
	caCert   *x509.Certificate
	caPEM    string
	signer   crypto.Signer
	defaults Profile
}

// NewSigningIssuer builds an issuer from a PEM-encoded CA certificate and the
// signer holding that CA's private key.
//
// The signer's public key must match the CA certificate's, otherwise every
// certificate issued would carry a signature the chain cannot verify. Checking
// once at construction turns that into a startup failure rather than a fleet
// of unusable certificates.
func NewSigningIssuer(caCertPEM string, signer crypto.Signer) (*SigningIssuer, error) {
	if signer == nil {
		return nil, rmerror.NewRMError(nil, "signer is nil")
	}

	caCert, err := ParseCertificatePEM(caCertPEM)
	if err != nil {
		return nil, err
	}
	if !caCert.IsCA {
		return nil, rmerror.NewRMError(nil, "issuing certificate is not a CA")
	}
	if len(caCert.SubjectKeyId) == 0 {
		// x509.CreateCertificate copies the parent's SKID into the leaf's
		// Authority Key Identifier. Without it the leaf would have no AKID,
		// which the Matter attestation profile requires.
		return nil, rmerror.NewRMError(nil, "issuing certificate has no Subject Key Identifier")
	}

	caPub, ok := caCert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, rmerror.NewRMError(nil, "issuing certificate key is not ECDSA")
	}
	signerPub, ok := signer.Public().(*ecdsa.PublicKey)
	if !ok {
		return nil, rmerror.NewRMError(nil, "signer key is not ECDSA")
	}
	if !caPub.Equal(signerPub) {
		return nil, rmerror.NewRMError(nil, "signer key does not match the issuing certificate")
	}

	return &SigningIssuer{
		caCert: caCert,
		caPEM:  caCertPEM,
		signer: signer,
	}, nil
}

// Issue produces a leaf certificate for pub.
//
// The extension set is fixed and identical across profiles:
//   - BasicConstraints, critical, CA=false
//   - KeyUsage, critical, digitalSignature
//   - Subject Key Identifier and Authority Key Identifier
//   - no ExtendedKeyUsage
//
// The omission of ExtendedKeyUsage is deliberate and is what lets one
// certificate serve as both the AWS IoT client certificate and a Matter
// attestation certificate: AWS IoT does not require clientAuth, and the Matter
// attestation profile does not permit an EKU at all (Req 8.5).
func (i *SigningIssuer) Issue(ctx context.Context, pub crypto.PublicKey, p Profile) (*Result, error) {
	if p.CommonName == "" {
		return nil, rmerror.NewRMError(nil, "profile common name is empty")
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, rmerror.NewRMError(nil, "subject key is not ECDSA")
	}
	if ecPub.Curve != elliptic.P256() {
		return nil, rmerror.NewRMError(nil, "subject key is not on the P-256 curve")
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	skid, err := subjectKeyID(ecPub)
	if err != nil {
		return nil, err
	}

	validity := p.Validity
	if validity == 0 {
		validity = DeviceCertValidity
	}
	now := time.Now().UTC()
	notAfter := now.Add(validity)

	// A leaf must never outlive its issuer, or chain validation breaks on the
	// CA's expiry date while the leaf still looks valid.
	//
	// Equal validity periods are not enough to guarantee this: the CA is minted
	// once and leaves are issued from then on, so a leaf issued even a minute
	// later would expire a minute after the CA. Clamping to the CA's own
	// notAfter is the only bound that holds however long the CA has been in
	// service — leaves issued near the end of its life simply get shorter
	// lifetimes, which is the correct behaviour.
	if notAfter.After(i.caCert.NotAfter) {
		notAfter = i.caCert.NotAfter
	}

	// CN is always the server-determined node ID; the operator subject adds
	// organizational attributes around it. Any explicit ExtraNames are appended
	// after the subject's own (e.g. its emailAddress RDN).
	subjectName := p.Subject.pkixName(p.CommonName)
	subjectName.ExtraNames = append(subjectName.ExtraNames, p.ExtraNames...)

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               subjectName,
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              notAfter,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  false,
		SubjectKeyId:          skid,
		// ExtKeyUsage intentionally unset — see the doc comment above.
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, i.caCert, ecPub, i.signer)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to create certificate")
	}

	return &Result{
		CertPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		ChainPEM: i.caPEM,
		CAID:     p.CAID,
	}, nil
}

// CACertPEM returns the issuing CA certificate.
func (i *SigningIssuer) CACertPEM() string {
	return i.caPEM
}

// NewSelfSignedCA mints a self-signed CA certificate for the key held by
// signer. Used once, out of band, to stand up the claiming CA whose private
// key lives in KMS; the resulting certificate is what gets registered with AWS
// IoT and shipped as the chain.
func NewSelfSignedCA(commonName string, subject Subject, signer crypto.Signer, validity time.Duration) (string, error) {
	pub, ok := signer.Public().(*ecdsa.PublicKey)
	if !ok {
		return "", rmerror.NewRMError(nil, "signer key is not ECDSA")
	}

	serial, err := randomSerial()
	if err != nil {
		return "", err
	}
	skid, err := subjectKeyID(pub)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               subject.pkixName(commonName),
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(validity),
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		SubjectKeyId:          skid,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, signer)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to create CA certificate")
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), nil
}

// ParseCertificatePEM decodes a single PEM certificate block.
func ParseCertificatePEM(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, rmerror.NewRMError(nil, "not a PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse certificate")
	}
	return cert, nil
}

// subjectKeyID computes the RFC 5280 method-1 key identifier: the SHA-1 of the
// subject public key BIT STRING. SHA-1 here is an identifier derivation, not a
// signature hash, so its collision weakness is not in play.
func subjectKeyID(pub *ecdsa.PublicKey) ([]byte, error) {
	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to marshal public key")
	}
	var info struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(spki, &info); err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse subject public key info")
	}
	sum := sha1.Sum(info.PublicKey.Bytes)
	return sum[:], nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), serialBits)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to generate serial number")
	}
	// A zero serial is invalid; shift the (vanishingly unlikely) zero to one.
	if serial.Sign() == 0 {
		serial = big.NewInt(1)
	}
	return serial, nil
}
