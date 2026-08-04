// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils"

	"github.com/go-playground/validator/v10"
)

func ValidateNew() *validator.Validate {
	validate := validator.New()
	for _, fn := range standardValidationFuncs {
		err := validate.RegisterValidation(fn.Name, fn.Fn)
		if err != nil {
			utils.Rlog.Error().Err(err).Str("validator", fn.Name).Msg("failed to register validation function")
		}
	}
	return validate
}

type ValidationFunc struct {
	Name string
	Fn   func(fl validator.FieldLevel) bool
}

func ValidateRequest(request interface{}, customValidationFuncs ...ValidationFunc) error {
	// TODO: This initialisation could optionally be cached
	validate := ValidateNew()
	for _, fn := range customValidationFuncs {
		if err := validate.RegisterValidation(fn.Name, fn.Fn); err != nil {
			return rmerror.NewRMError(err, "failed to register validation function")
		}
	}
	if err := validate.Struct(request); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			var errorMessages []string
			for _, validationError := range validationErrors {
				errorMessages = append(errorMessages, fmt.Sprintf("Key: '%s' Error:Field validation for '%s' failed on the '%s' tag", validationError.Namespace(), validationError.Field(), validationError.Tag()))
			}
			return rmerror.NewRMError(err, fmt.Sprintf("validation failed: %s", strings.Join(errorMessages, "; ")))
		}
		return rmerror.NewRMError(err, "validation failed")
	}

	return nil
}

var standardValidationFuncs = []ValidationFunc{
	validateTag,
	validateCert,
	validateEmailOrPhone,
	validateStrongPassword,
	validateE164,
	// Add more standard validation functions here
}

// Standard validator function for tags in key:value format
var validateTag = ValidationFunc{
	Name: "std_tag_format",
	Fn: func(fl validator.FieldLevel) bool {
		tag := fl.Field().String()
		if tag == "" {
			return false
		}
		parts := strings.SplitN(tag, ":", 2)
		return len(parts) == 2 && parts[0] != "" && parts[1] != ""
	},
}

// Standard validator function for IoT Core certificates
var validateCert = ValidationFunc{
	Name: "std_iot_cert",
	Fn: func(fl validator.FieldLevel) bool {
		cert := fl.Field().String()
		if cert == "" {
			utils.Rlog.Debug().Msg("certificate is empty")
			return false
		}
		certBlock, _ := pem.Decode([]byte(cert))
		if certBlock == nil {
			utils.Rlog.Debug().Str("cert", cert).Msg("failed to decode PEM certificate")
			return false
		}

		// Verify the PEM block type
		if certBlock.Type != "CERTIFICATE" {
			utils.Rlog.Debug().Str("type", certBlock.Type).Msg("invalid PEM block type")
			return false
		}

		parsedCert, err := x509.ParseCertificate(certBlock.Bytes)
		if err != nil {
			utils.Rlog.Debug().Err(err).Msg("failed to parse certificate")
			return false
		}

		// Validate Subject Common Name is not empty
		if parsedCert.Subject.CommonName == "" {
			utils.Rlog.Debug().Msg("certificate subject common name is empty")
			return false
		}

		// Validate certificate key type and size: RSA2048 or ECC NIST P-256/P-384/P-521
		pub := parsedCert.PublicKey
		if pub == nil {
			utils.Rlog.Debug().Msg("certificate public key is nil")
			return false
		}

		switch key := pub.(type) {
		case *rsa.PublicKey:
			if key.N.BitLen() != 2048 {
				utils.Rlog.Debug().Int("key_size", key.N.BitLen()).Msg("invalid RSA key size, must be 2048 bits")
				return false
			}
		case *ecdsa.PublicKey:
			curve := key.Curve
			switch curve {
			case elliptic.P256(), elliptic.P384(), elliptic.P521():
				// Valid NIST curves
			default:
				utils.Rlog.Debug().Str("curve", curve.Params().Name).Msg("invalid EC curve, must be NIST P-256/P-384/P-521")
				return false
			}
		default:
			utils.Rlog.Debug().Str("key_type", fmt.Sprintf("%T", pub)).Msg("invalid key type, must be RSA-2048 or NIST P-256/P-384/P-521")
			return false
		}

		return true
	},
}

var validateEmailOrPhone = ValidationFunc{
	Name: "email_or_phone",
	Fn: func(fl validator.FieldLevel) bool {
		username := fl.Field().String()
		if username == "" {
			return false
		}

		return ValidateEmail(username) || ValidatePhone(username)
	},
}

var validateStrongPassword = ValidationFunc{
	Name: "strong_password",
	Fn: func(fl validator.FieldLevel) bool {
		password := fl.Field().String()
		if len(password) < 8 {
			return false
		}
		// Check for at least one uppercase, one lowercase, one digit
		hasUpper, hasLower, hasDigit := false, false, false
		for _, char := range password {
			if char >= 'A' && char <= 'Z' {
				hasUpper = true
			} else if char >= 'a' && char <= 'z' {
				hasLower = true
			} else if char >= '0' && char <= '9' {
				hasDigit = true
			}
		}
		return hasUpper && hasLower && hasDigit
	},
}

var validateE164 = ValidationFunc{
	Name: "e164",
	Fn: func(fl validator.FieldLevel) bool {
		phone := fl.Field().String()
		return ValidatePhone(phone)
	},
}

func ValidateEmail(email string) bool {
	return strings.Contains(email, "@") && strings.Contains(email, ".") && email == strings.ToLower(email) //It is clients' responsibility to ensure lowercase emails
}

func ValidatePhone(phone string) bool {
	if !strings.HasPrefix(phone, "+") || len(phone) < 8 {
		return false
	}

	for _, char := range phone[1:] {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
