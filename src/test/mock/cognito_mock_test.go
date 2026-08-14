// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"context"
	"errors"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CognitoProviderMock", func() {
	var (
		cognitoMock    *mock.CognitoProviderMock
		ctx            context.Context
		testUserPoolID = "us-east-1_TestPool"
		testClientID   = "test-client-id"
		testUsername   = "testuser"
		testEmail      = "test@example.com"
		testPassword   = "TestPassword123!"
	)

	BeforeEach(func() {
		cognitoMock = mock.NewCognitoProviderMock()
		ctx = context.Background()
	})

	Describe("NewCognitoProviderMock", func() {
		It("should create a new mock with default values", func() {
			mock := mock.NewCognitoProviderMock()
			Expect(mock).ToNot(BeNil())
			Expect(mock.DefaultConfirmationCode).To(Equal("123456"))
			Expect(mock.DefaultResetCode).To(Equal("654321"))
			Expect(mock.TokenExpirationMinutes).To(Equal(60))
			Expect(mock.DefaultUserPoolID).To(Equal("us-east-1_TestPool"))
		})
	})

	Describe("GetUserByUsername", func() {
		BeforeEach(func() {
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)
		})

		It("should retrieve existing user", func() {
			user := cognitoMock.GetUserByUsername(testUserPoolID, testUsername)

			Expect(user).ToNot(BeNil())
			Expect(user.Username).To(Equal(testUsername))
			Expect(user.Email).To(Equal(testEmail))
		})

		It("should return nil for non-existent user", func() {
			user := cognitoMock.GetUserByUsername(testUserPoolID, "nonexistent")
			Expect(user).To(BeNil())
		})

		It("should return nil for non-existent user pool", func() {
			user := cognitoMock.GetUserByUsername("nonexistent-pool", testUsername)
			Expect(user).To(BeNil())
		})
	})

	Describe("SignUp", func() {
		var signUpInput *cognitoidentityprovider.SignUpInput

		BeforeEach(func() {
			signUpInput = &cognitoidentityprovider.SignUpInput{
				ClientId: aws.String(testClientID),
				Username: aws.String(testUsername),
				Password: aws.String(testPassword),
				UserAttributes: []types.AttributeType{
					{
						Name:  aws.String("email"),
						Value: aws.String(testEmail),
					},
				},
			}
		})

		It("should sign up a new user successfully", func() {
			result, err := cognitoMock.SignUp(ctx, signUpInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.UserSub).ToNot(BeNil())
			Expect(*result.UserSub).ToNot(BeEmpty())
			Expect(result.UserConfirmed).To(BeFalse())

			// Verify user was stored
			user := cognitoMock.GetUserByUsername(cognitoMock.DefaultUserPoolID, testUsername)
			Expect(user).ToNot(BeNil())
			Expect(user.IsConfirmed).To(BeFalse())
		})

		It("should return error when user already exists", func() {
			// First signup
			_, err := cognitoMock.SignUp(ctx, signUpInput)
			Expect(err).ToNot(HaveOccurred())

			// Second signup with same username
			_, err = cognitoMock.SignUp(ctx, signUpInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user already exists"))
		})

		It("should return injected error", func() {
			testError := errors.New("signup error")
			cognitoMock.SetErrorForMethod("signup", testError)

			_, err := cognitoMock.SignUp(ctx, signUpInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("ConfirmSignUp", func() {
		var confirmInput *cognitoidentityprovider.ConfirmSignUpInput

		BeforeEach(func() {
			// Create unconfirmed user
			signUpInput := &cognitoidentityprovider.SignUpInput{
				ClientId: aws.String(testClientID),
				Username: aws.String(testUsername),
				Password: aws.String(testPassword),
			}
			_, err := cognitoMock.SignUp(ctx, signUpInput)
			Expect(err).ToNot(HaveOccurred())

			confirmInput = &cognitoidentityprovider.ConfirmSignUpInput{
				ClientId:         aws.String(testClientID),
				Username:         aws.String(testUsername),
				ConfirmationCode: aws.String(cognitoMock.DefaultConfirmationCode),
			}
		})

		It("should confirm user successfully", func() {
			result, err := cognitoMock.ConfirmSignUp(ctx, confirmInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify user is confirmed
			user := cognitoMock.GetUserByUsername(cognitoMock.DefaultUserPoolID, testUsername)
			Expect(user.IsConfirmed).To(BeTrue())
		})

		It("should return error for invalid confirmation code", func() {
			confirmInput.ConfirmationCode = aws.String("wrong-code")

			_, err := cognitoMock.ConfirmSignUp(ctx, confirmInput)
			Expect(err).To(HaveOccurred())
			// Typed like the real service: callers branch on the exception, not on the text.
			var codeMismatch *types.CodeMismatchException
			Expect(errors.As(err, &codeMismatch)).To(BeTrue())
		})

		It("should return error for non-existent user", func() {
			confirmInput.Username = aws.String("nonexistent")

			_, err := cognitoMock.ConfirmSignUp(ctx, confirmInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user not found"))
		})

		It("should return error for already confirmed user", func() {
			// First confirmation
			_, err := cognitoMock.ConfirmSignUp(ctx, confirmInput)
			Expect(err).ToNot(HaveOccurred())

			// Second confirmation
			_, err = cognitoMock.ConfirmSignUp(ctx, confirmInput)
			Expect(err).To(HaveOccurred())
			// Measured: a spent-but-matching code on a confirmed account answers ExpiredCode.
			var expired *types.ExpiredCodeException
			Expect(errors.As(err, &expired)).To(BeTrue())
		})

		It("should return injected error", func() {
			testError := errors.New("confirm signup error")
			cognitoMock.SetErrorForMethod("confirmsignup", testError)

			_, err := cognitoMock.ConfirmSignUp(ctx, confirmInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("ResendConfirmationCode", func() {
		var resendInput *cognitoidentityprovider.ResendConfirmationCodeInput

		BeforeEach(func() {
			// Create unconfirmed user
			signUpInput := &cognitoidentityprovider.SignUpInput{
				ClientId: aws.String(testClientID),
				Username: aws.String(testUsername),
				Password: aws.String(testPassword),
				UserAttributes: []types.AttributeType{
					{Name: aws.String("email"), Value: aws.String(testEmail)},
				},
			}
			_, err := cognitoMock.SignUp(ctx, signUpInput)
			Expect(err).ToNot(HaveOccurred())

			resendInput = &cognitoidentityprovider.ResendConfirmationCodeInput{
				ClientId: aws.String(testClientID),
				Username: aws.String(testUsername),
			}
		})

		It("should resend code for unconfirmed user", func() {
			result, err := cognitoMock.ResendConfirmationCode(ctx, resendInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.CodeDeliveryDetails).ToNot(BeNil())
		})

		It("should return error for non-existent user", func() {
			resendInput.Username = aws.String("nonexistent")

			_, err := cognitoMock.ResendConfirmationCode(ctx, resendInput)
			Expect(err).To(HaveOccurred())
		})

		It("should return error for already confirmed user", func() {
			// Confirm the user first
			confirmInput := &cognitoidentityprovider.ConfirmSignUpInput{
				ClientId:         aws.String(testClientID),
				Username:         aws.String(testUsername),
				ConfirmationCode: aws.String(cognitoMock.DefaultConfirmationCode),
			}
			_, err := cognitoMock.ConfirmSignUp(ctx, confirmInput)
			Expect(err).ToNot(HaveOccurred())

			_, err = cognitoMock.ResendConfirmationCode(ctx, resendInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user already confirmed"))
		})

		It("should return injected error", func() {
			testError := errors.New("resend error")
			cognitoMock.ResendConfirmationCodeError = testError

			_, err := cognitoMock.ResendConfirmationCode(ctx, resendInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("InitiateAuth - User Password Auth", func() {
		var authInput *cognitoidentityprovider.InitiateAuthInput

		BeforeEach(func() {
			// Create confirmed user
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			authInput = &cognitoidentityprovider.InitiateAuthInput{
				ClientId: aws.String(testClientID),
				AuthFlow: types.AuthFlowTypeUserPasswordAuth,
				AuthParameters: map[string]string{
					"USERNAME": testUsername,
					"PASSWORD": testPassword,
				},
			}
		})

		It("should authenticate user successfully", func() {
			result, err := cognitoMock.InitiateAuth(ctx, authInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.AuthenticationResult).ToNot(BeNil())
			Expect(result.AuthenticationResult.AccessToken).ToNot(BeNil())
			Expect(result.AuthenticationResult.RefreshToken).ToNot(BeNil())
			Expect(result.AuthenticationResult.IdToken).ToNot(BeNil())
			Expect(result.AuthenticationResult.ExpiresIn).To(Equal(int32(3600)))
			Expect(*result.AuthenticationResult.TokenType).To(Equal("Bearer"))

			// Verify session was created
			session := cognitoMock.GetSessionByToken(*result.AuthenticationResult.AccessToken)
			Expect(session).ToNot(BeNil())
			Expect(session.Username).To(Equal(testUsername))
		})

		It("should return error for invalid credentials", func() {
			authInput.AuthParameters["PASSWORD"] = "wrong-password"

			_, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Incorrect username or password"))
		})

		It("should return error for unconfirmed user", func() {
			// Create unconfirmed user
			cognitoMock.AddTestUserDirect(testUserPoolID, "unconfirmed", testEmail, testPassword, false)
			authInput.AuthParameters["USERNAME"] = "unconfirmed"

			_, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).To(HaveOccurred())
			var notConfirmed *types.UserNotConfirmedException
			Expect(errors.As(err, &notConfirmed)).To(BeTrue())
		})

		It("should return error for disabled user", func() {
			user := cognitoMock.GetUserByUsername(testUserPoolID, testUsername)
			user.IsEnabled = false

			_, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user disabled"))
		})

		It("should return error for missing username", func() {
			delete(authInput.AuthParameters, "USERNAME")

			_, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("username required"))
		})

		It("should return error for missing password", func() {
			delete(authInput.AuthParameters, "PASSWORD")

			_, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("password required"))
		})

		It("should return injected error", func() {
			testError := errors.New("initiate auth error")
			cognitoMock.SetErrorForMethod("initiateauth", testError)

			_, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("InitiateAuth - Refresh Token Auth", func() {
		var (
			refreshTokenInput *cognitoidentityprovider.InitiateAuthInput
			originalTokens    *cognitoidentityprovider.InitiateAuthOutput
		)

		BeforeEach(func() {
			// Create confirmed user and get initial tokens
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			authInput := &cognitoidentityprovider.InitiateAuthInput{
				ClientId: aws.String(testClientID),
				AuthFlow: types.AuthFlowTypeUserPasswordAuth,
				AuthParameters: map[string]string{
					"USERNAME": testUsername,
					"PASSWORD": testPassword,
				},
			}

			var err error
			originalTokens, err = cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).ToNot(HaveOccurred())

			refreshTokenInput = &cognitoidentityprovider.InitiateAuthInput{
				ClientId: aws.String(testClientID),
				AuthFlow: types.AuthFlowTypeRefreshTokenAuth,
				AuthParameters: map[string]string{
					"REFRESH_TOKEN": *originalTokens.AuthenticationResult.RefreshToken,
				},
			}
		})

		It("should refresh tokens successfully", func() {
			result, err := cognitoMock.InitiateAuth(ctx, refreshTokenInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.AuthenticationResult).ToNot(BeNil())
			Expect(result.AuthenticationResult.AccessToken).ToNot(BeNil())
			Expect(result.AuthenticationResult.RefreshToken).ToNot(BeNil())
			Expect(result.AuthenticationResult.IdToken).ToNot(BeNil())

			// Verify new access token is different
			Expect(*result.AuthenticationResult.AccessToken).ToNot(Equal(*originalTokens.AuthenticationResult.AccessToken))
			// Verify refresh token is the same
			Expect(*result.AuthenticationResult.RefreshToken).To(Equal(*originalTokens.AuthenticationResult.RefreshToken))

			// Verify old session is removed and new session exists
			oldSession := cognitoMock.GetSessionByToken(*originalTokens.AuthenticationResult.AccessToken)
			Expect(oldSession).To(BeNil())

			newSession := cognitoMock.GetSessionByToken(*result.AuthenticationResult.AccessToken)
			Expect(newSession).ToNot(BeNil())
			Expect(newSession.Username).To(Equal(testUsername))
		})

		It("should return error for invalid refresh token", func() {
			refreshTokenInput.AuthParameters["REFRESH_TOKEN"] = "invalid-token"

			_, err := cognitoMock.InitiateAuth(ctx, refreshTokenInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid refresh token"))
		})

		It("should return error for missing refresh token", func() {
			delete(refreshTokenInput.AuthParameters, "REFRESH_TOKEN")

			_, err := cognitoMock.InitiateAuth(ctx, refreshTokenInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("refresh token required"))
		})
	})

	Describe("ForgotPassword", func() {
		var forgotInput *cognitoidentityprovider.ForgotPasswordInput

		BeforeEach(func() {
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			forgotInput = &cognitoidentityprovider.ForgotPasswordInput{
				ClientId: aws.String(testClientID),
				Username: aws.String(testUsername),
			}
		})

		It("should initiate password reset successfully", func() {
			result, err := cognitoMock.ForgotPassword(ctx, forgotInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify reset code was set
			user := cognitoMock.GetUserByUsername(cognitoMock.DefaultUserPoolID, testUsername)
			Expect(user.ResetCode).To(Equal(cognitoMock.DefaultResetCode))
		})

		It("should return error for non-existent user", func() {
			forgotInput.Username = aws.String("nonexistent")

			_, err := cognitoMock.ForgotPassword(ctx, forgotInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user not found"))
		})

		It("should return injected error", func() {
			testError := errors.New("forgot password error")
			cognitoMock.SetErrorForMethod("forgotpassword", testError)

			_, err := cognitoMock.ForgotPassword(ctx, forgotInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("ConfirmForgotPassword", func() {
		var confirmForgotInput *cognitoidentityprovider.ConfirmForgotPasswordInput
		newPassword := "NewPassword123!"

		BeforeEach(func() {
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			// Initiate forgot password
			forgotInput := &cognitoidentityprovider.ForgotPasswordInput{
				ClientId: aws.String(testClientID),
				Username: aws.String(testUsername),
			}
			_, err := cognitoMock.ForgotPassword(ctx, forgotInput)
			Expect(err).ToNot(HaveOccurred())

			confirmForgotInput = &cognitoidentityprovider.ConfirmForgotPasswordInput{
				ClientId:         aws.String(testClientID),
				Username:         aws.String(testUsername),
				ConfirmationCode: aws.String(cognitoMock.DefaultResetCode),
				Password:         aws.String(newPassword),
			}
		})

		It("should reset password successfully", func() {
			result, err := cognitoMock.ConfirmForgotPassword(ctx, confirmForgotInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify password was changed
			user := cognitoMock.GetUserByUsername(cognitoMock.DefaultUserPoolID, testUsername)
			Expect(user.Password).To(Equal(newPassword))
			Expect(user.ResetCode).To(BeEmpty())
		})

		It("should return error for invalid confirmation code", func() {
			confirmForgotInput.ConfirmationCode = aws.String("wrong-code")

			_, err := cognitoMock.ConfirmForgotPassword(ctx, confirmForgotInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid confirmation code"))
		})

		It("should return error for non-existent user", func() {
			confirmForgotInput.Username = aws.String("nonexistent")

			_, err := cognitoMock.ConfirmForgotPassword(ctx, confirmForgotInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user not found"))
		})

		It("should return injected error", func() {
			testError := errors.New("confirm forgot password error")
			cognitoMock.SetErrorForMethod("confirmforgotpassword", testError)

			_, err := cognitoMock.ConfirmForgotPassword(ctx, confirmForgotInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("ChangePassword", func() {
		var (
			changePasswordInput *cognitoidentityprovider.ChangePasswordInput
			accessToken         string
			newPassword         = "NewPassword123!"
		)

		BeforeEach(func() {
			// Create user and authenticate to get access token
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			authInput := &cognitoidentityprovider.InitiateAuthInput{
				ClientId: aws.String(testClientID),
				AuthFlow: types.AuthFlowTypeUserPasswordAuth,
				AuthParameters: map[string]string{
					"USERNAME": testUsername,
					"PASSWORD": testPassword,
				},
			}

			result, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).ToNot(HaveOccurred())
			accessToken = *result.AuthenticationResult.AccessToken

			changePasswordInput = &cognitoidentityprovider.ChangePasswordInput{
				AccessToken:      aws.String(accessToken),
				PreviousPassword: aws.String(testPassword),
				ProposedPassword: aws.String(newPassword),
			}
		})

		It("should change password successfully", func() {
			result, err := cognitoMock.ChangePassword(ctx, changePasswordInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify password was changed
			user := cognitoMock.GetUserByUsername(testUserPoolID, testUsername)
			Expect(user.Password).To(Equal(newPassword))
		})

		It("should return error for invalid access token", func() {
			changePasswordInput.AccessToken = aws.String("invalid-token")

			_, err := cognitoMock.ChangePassword(ctx, changePasswordInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid access token"))
		})

		It("should return error for incorrect previous password", func() {
			changePasswordInput.PreviousPassword = aws.String("wrong-password")

			_, err := cognitoMock.ChangePassword(ctx, changePasswordInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("incorrect previous password"))
		})

		It("should return injected error", func() {
			testError := errors.New("change password error")
			cognitoMock.SetErrorForMethod("changepassword", testError)

			_, err := cognitoMock.ChangePassword(ctx, changePasswordInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("GetUser", func() {
		var (
			getUserInput *cognitoidentityprovider.GetUserInput
			accessToken  string
		)

		BeforeEach(func() {
			// Create user and authenticate to get access token
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			authInput := &cognitoidentityprovider.InitiateAuthInput{
				ClientId: aws.String(testClientID),
				AuthFlow: types.AuthFlowTypeUserPasswordAuth,
				AuthParameters: map[string]string{
					"USERNAME": testUsername,
					"PASSWORD": testPassword,
				},
			}

			result, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).ToNot(HaveOccurred())
			accessToken = *result.AuthenticationResult.AccessToken

			getUserInput = &cognitoidentityprovider.GetUserInput{
				AccessToken: aws.String(accessToken),
			}
		})

		It("should get user successfully", func() {
			result, err := cognitoMock.GetUser(ctx, getUserInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(*result.Username).To(Equal(testUsername))
			Expect(len(result.UserAttributes)).To(BeNumerically(">", 0))

			// Check email attribute
			var emailFound bool
			for _, attr := range result.UserAttributes {
				if *attr.Name == "email" && *attr.Value == testEmail {
					emailFound = true
					break
				}
			}
			Expect(emailFound).To(BeTrue())
		})

		It("should return error for invalid access token", func() {
			getUserInput.AccessToken = aws.String("invalid-token")

			_, err := cognitoMock.GetUser(ctx, getUserInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid access token"))
		})

		It("should return injected error", func() {
			testError := errors.New("get user error")
			cognitoMock.SetErrorForMethod("getuser", testError)

			_, err := cognitoMock.GetUser(ctx, getUserInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("RevokeToken", func() {
		var (
			revokeTokenInput *cognitoidentityprovider.RevokeTokenInput
			refreshToken     string
			accessToken      string
		)

		BeforeEach(func() {
			// Create user and authenticate to get tokens
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			authInput := &cognitoidentityprovider.InitiateAuthInput{
				ClientId: aws.String(testClientID),
				AuthFlow: types.AuthFlowTypeUserPasswordAuth,
				AuthParameters: map[string]string{
					"USERNAME": testUsername,
					"PASSWORD": testPassword,
				},
			}

			result, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).ToNot(HaveOccurred())
			refreshToken = *result.AuthenticationResult.RefreshToken
			accessToken = *result.AuthenticationResult.AccessToken

			revokeTokenInput = &cognitoidentityprovider.RevokeTokenInput{
				ClientId: aws.String(testClientID),
				Token:    aws.String(refreshToken),
			}
		})

		It("should revoke token successfully", func() {
			result, err := cognitoMock.RevokeToken(ctx, revokeTokenInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify session was deleted
			session := cognitoMock.GetSessionByToken(accessToken)
			Expect(session).To(BeNil())
		})

		It("should return error for invalid refresh token", func() {
			revokeTokenInput.Token = aws.String("invalid-token")

			_, err := cognitoMock.RevokeToken(ctx, revokeTokenInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid refresh token"))
		})

		It("should return injected error", func() {
			testError := errors.New("revoke token error")
			cognitoMock.SetErrorForMethod("revoketoken", testError)

			_, err := cognitoMock.RevokeToken(ctx, revokeTokenInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("GlobalSignOut", func() {
		var (
			globalSignOutInput *cognitoidentityprovider.GlobalSignOutInput
			accessToken        string
		)

		BeforeEach(func() {
			// Create user and authenticate to get access token
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			authInput := &cognitoidentityprovider.InitiateAuthInput{
				ClientId: aws.String(testClientID),
				AuthFlow: types.AuthFlowTypeUserPasswordAuth,
				AuthParameters: map[string]string{
					"USERNAME": testUsername,
					"PASSWORD": testPassword,
				},
			}

			result, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).ToNot(HaveOccurred())
			accessToken = *result.AuthenticationResult.AccessToken

			globalSignOutInput = &cognitoidentityprovider.GlobalSignOutInput{
				AccessToken: aws.String(accessToken),
			}
		})

		It("should sign out globally successfully", func() {
			result, err := cognitoMock.GlobalSignOut(ctx, globalSignOutInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify session was deleted
			session := cognitoMock.GetSessionByToken(accessToken)
			Expect(session).To(BeNil())
		})

		It("should return error for invalid access token", func() {
			globalSignOutInput.AccessToken = aws.String("invalid-token")

			_, err := cognitoMock.GlobalSignOut(ctx, globalSignOutInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid access token"))
		})

		It("should return injected error", func() {
			testError := errors.New("global sign out error")
			cognitoMock.SetErrorForMethod("globalsignout", testError)

			_, err := cognitoMock.GlobalSignOut(ctx, globalSignOutInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("UpdateUserPoolClient", func() {
		var updateInput *cognitoidentityprovider.UpdateUserPoolClientInput

		BeforeEach(func() {
			updateInput = &cognitoidentityprovider.UpdateUserPoolClientInput{
				UserPoolId: aws.String(testUserPoolID),
				ClientId:   aws.String(testClientID),
				AllowedOAuthFlows: []types.OAuthFlowType{
					types.OAuthFlowTypeCode,
				},
				AllowedOAuthScopes:              []string{"openid", "email"},
				AllowedOAuthFlowsUserPoolClient: true,
				SupportedIdentityProviders:      []string{"COGNITO"},
			}
		})

		It("should update user pool client successfully", func() {
			result, err := cognitoMock.UpdateUserPoolClient(ctx, updateInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.UserPoolClient).ToNot(BeNil())
			Expect(result.UserPoolClient.AllowedOAuthFlows).To(Equal(updateInput.AllowedOAuthFlows))
			Expect(result.UserPoolClient.AllowedOAuthScopes).To(Equal(updateInput.AllowedOAuthScopes))
		})

		It("should return injected error", func() {
			testError := errors.New("update user pool client error")
			cognitoMock.UpdateUserPoolClientError = testError

			_, err := cognitoMock.UpdateUserPoolClient(ctx, updateInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("DescribeUserPoolClient", func() {
		var describeInput *cognitoidentityprovider.DescribeUserPoolClientInput

		BeforeEach(func() {
			// First create a client
			updateInput := &cognitoidentityprovider.UpdateUserPoolClientInput{
				UserPoolId: aws.String(testUserPoolID),
				ClientId:   aws.String(testClientID),
				AllowedOAuthFlows: []types.OAuthFlowType{
					types.OAuthFlowTypeCode,
				},
				AllowedOAuthScopes:              []string{"openid", "email"},
				AllowedOAuthFlowsUserPoolClient: true,
				SupportedIdentityProviders:      []string{"COGNITO"},
			}
			_, err := cognitoMock.UpdateUserPoolClient(ctx, updateInput)
			Expect(err).ToNot(HaveOccurred())

			describeInput = &cognitoidentityprovider.DescribeUserPoolClientInput{
				UserPoolId: aws.String(testUserPoolID),
				ClientId:   aws.String(testClientID),
			}
		})

		It("should describe user pool client successfully", func() {
			result, err := cognitoMock.DescribeUserPoolClient(ctx, describeInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.UserPoolClient).ToNot(BeNil())
		})

		It("should return error for non-existent user pool", func() {
			describeInput.UserPoolId = aws.String("nonexistent-pool")

			_, err := cognitoMock.DescribeUserPoolClient(ctx, describeInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user pool not found"))
		})

		It("should return error for non-existent client", func() {
			describeInput.ClientId = aws.String("nonexistent-client")

			_, err := cognitoMock.DescribeUserPoolClient(ctx, describeInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("client not found"))
		})

		It("should return injected error", func() {
			testError := errors.New("describe user pool client error")
			cognitoMock.DescribeUserPoolClientError = testError

			_, err := cognitoMock.DescribeUserPoolClient(ctx, describeInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("RespondToAuthChallenge", func() {
		var challengeInput *cognitoidentityprovider.RespondToAuthChallengeInput

		BeforeEach(func() {
			challengeInput = &cognitoidentityprovider.RespondToAuthChallengeInput{
				ClientId:      aws.String(testClientID),
				ChallengeName: types.ChallengeNameTypeNewPasswordRequired,
				Session:       aws.String("test-session"),
				ChallengeResponses: map[string]string{
					"USERNAME":     testUsername,
					"NEW_PASSWORD": "NewPassword123!",
				},
			}
		})

		It("should respond to auth challenge successfully", func() {
			result, err := cognitoMock.RespondToAuthChallenge(ctx, challengeInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.AuthenticationResult).ToNot(BeNil())
			Expect(result.AuthenticationResult.AccessToken).ToNot(BeNil())
			Expect(result.AuthenticationResult.RefreshToken).ToNot(BeNil())
			Expect(result.AuthenticationResult.IdToken).ToNot(BeNil())
		})

		It("should return injected error", func() {
			testError := errors.New("respond to auth challenge error")
			cognitoMock.SetErrorForMethod("respondtoauthchallenge", testError)

			_, err := cognitoMock.RespondToAuthChallenge(ctx, challengeInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("Session Management", func() {
		var (
			accessToken  string
			refreshToken string
		)

		BeforeEach(func() {
			// Create user and authenticate to get tokens
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			authInput := &cognitoidentityprovider.InitiateAuthInput{
				ClientId: aws.String(testClientID),
				AuthFlow: types.AuthFlowTypeUserPasswordAuth,
				AuthParameters: map[string]string{
					"USERNAME": testUsername,
					"PASSWORD": testPassword,
				},
			}

			result, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).ToNot(HaveOccurred())
			accessToken = *result.AuthenticationResult.AccessToken
			refreshToken = *result.AuthenticationResult.RefreshToken
		})

		Describe("GetSessionByToken", func() {
			It("should retrieve session by access token", func() {
				session := cognitoMock.GetSessionByToken(accessToken)

				Expect(session).ToNot(BeNil())
				Expect(session.Username).To(Equal(testUsername))
				Expect(session.AccessToken).To(Equal(accessToken))
				Expect(session.RefreshToken).To(Equal(refreshToken))
			})

			It("should return nil for invalid token", func() {
				session := cognitoMock.GetSessionByToken("invalid-token")
				Expect(session).To(BeNil())
			})
		})

		Describe("GetSessionByRefreshToken", func() {
			It("should retrieve session by refresh token", func() {
				session := cognitoMock.GetSessionByRefreshToken(refreshToken)

				Expect(session).ToNot(BeNil())
				Expect(session.Username).To(Equal(testUsername))
				Expect(session.AccessToken).To(Equal(accessToken))
				Expect(session.RefreshToken).To(Equal(refreshToken))
			})

			It("should return nil for invalid refresh token", func() {
				session := cognitoMock.GetSessionByRefreshToken("invalid-token")
				Expect(session).To(BeNil())
			})
		})
	})

	Describe("Token Expiration", func() {
		It("should handle expired tokens correctly", func() {
			// Set very short expiration for testing
			cognitoMock.TokenExpirationMinutes = 0 // This will make tokens expire immediately

			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			authInput := &cognitoidentityprovider.InitiateAuthInput{
				ClientId: aws.String(testClientID),
				AuthFlow: types.AuthFlowTypeUserPasswordAuth,
				AuthParameters: map[string]string{
					"USERNAME": testUsername,
					"PASSWORD": testPassword,
				},
			}

			result, err := cognitoMock.InitiateAuth(ctx, authInput)
			Expect(err).ToNot(HaveOccurred())

			// Wait a moment to ensure token expires
			time.Sleep(time.Millisecond * 10)

			// Try to use expired token
			getUserInput := &cognitoidentityprovider.GetUserInput{
				AccessToken: result.AuthenticationResult.AccessToken,
			}

			_, err = cognitoMock.GetUser(ctx, getUserInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("access token expired"))
		})
	})

	Describe("AdminCreateUser", func() {
		var createInput *cognitoidentityprovider.AdminCreateUserInput

		BeforeEach(func() {
			createInput = &cognitoidentityprovider.AdminCreateUserInput{
				UserPoolId: aws.String(testUserPoolID),
				Username:   aws.String(testUsername),
				UserAttributes: []types.AttributeType{
					{
						Name:  aws.String("email"),
						Value: aws.String(testEmail),
					},
					{
						Name:  aws.String("email_verified"),
						Value: aws.String("true"),
					},
				},
				MessageAction: types.MessageActionTypeSuppress,
			}
		})

		It("should create user successfully", func() {
			result, err := cognitoMock.AdminCreateUser(ctx, createInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.User).ToNot(BeNil())
			Expect(*result.User.Username).To(Equal(testUsername))
			Expect(result.User.UserStatus).To(Equal(types.UserStatusTypeConfirmed))
			Expect(result.User.Enabled).To(BeTrue())

			// Verify user was stored
			user := cognitoMock.GetUserByUsername(testUserPoolID, testUsername)
			Expect(user).ToNot(BeNil())
			Expect(user.IsConfirmed).To(BeTrue())
			Expect(user.Email).To(Equal(testEmail))
		})

		It("should create user with temporary password", func() {
			tempPassword := "TempPassword123!"
			createInput.TemporaryPassword = aws.String(tempPassword)

			result, err := cognitoMock.AdminCreateUser(ctx, createInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify temporary password was set
			user := cognitoMock.GetUserByUsername(testUserPoolID, testUsername)
			Expect(user.Password).To(Equal(tempPassword))
		})

		It("should create user with phone number", func() {
			testPhone := "+1234567890"
			createInput.UserAttributes = append(createInput.UserAttributes, types.AttributeType{
				Name:  aws.String("phone_number"),
				Value: aws.String(testPhone),
			})

			result, err := cognitoMock.AdminCreateUser(ctx, createInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify phone number was stored
			user := cognitoMock.GetUserByUsername(testUserPoolID, testUsername)
			Expect(user.PhoneNumber).To(Equal(testPhone))
		})

		It("should return error when user already exists", func() {
			// First creation
			_, err := cognitoMock.AdminCreateUser(ctx, createInput)
			Expect(err).ToNot(HaveOccurred())

			// Second creation with same username
			_, err = cognitoMock.AdminCreateUser(ctx, createInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user already exists"))
		})

		It("should return injected error", func() {
			testError := errors.New("admin create user error")
			cognitoMock.AdminCreateUserError = testError

			_, err := cognitoMock.AdminCreateUser(ctx, createInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("AdminSetUserPassword", func() {
		var setPasswordInput *cognitoidentityprovider.AdminSetUserPasswordInput
		newPassword := "NewPassword123!"

		BeforeEach(func() {
			// Create user first
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			setPasswordInput = &cognitoidentityprovider.AdminSetUserPasswordInput{
				UserPoolId: aws.String(testUserPoolID),
				Username:   aws.String(testUsername),
				Password:   aws.String(newPassword),
				Permanent:  true,
			}
		})

		It("should set user password successfully", func() {
			result, err := cognitoMock.AdminSetUserPassword(ctx, setPasswordInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify password was changed
			user := cognitoMock.GetUserByUsername(testUserPoolID, testUsername)
			Expect(user.Password).To(Equal(newPassword))
		})

		It("should set password for user created via AdminCreateUser", func() {
			// Create user via AdminCreateUser
			createInput := &cognitoidentityprovider.AdminCreateUserInput{
				UserPoolId: aws.String(testUserPoolID),
				Username:   aws.String("newuser"),
				UserAttributes: []types.AttributeType{
					{
						Name:  aws.String("email"),
						Value: aws.String("newuser@example.com"),
					},
				},
				MessageAction: types.MessageActionTypeSuppress,
			}
			_, err := cognitoMock.AdminCreateUser(ctx, createInput)
			Expect(err).ToNot(HaveOccurred())

			// Set password
			setPasswordInput.Username = aws.String("newuser")
			result, err := cognitoMock.AdminSetUserPassword(ctx, setPasswordInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify password was set
			user := cognitoMock.GetUserByUsername(testUserPoolID, "newuser")
			Expect(user.Password).To(Equal(newPassword))
		})

		It("should return error for non-existent user", func() {
			setPasswordInput.Username = aws.String("nonexistent")

			_, err := cognitoMock.AdminSetUserPassword(ctx, setPasswordInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user not found"))
		})

		It("should return error for non-existent user pool", func() {
			setPasswordInput.UserPoolId = aws.String("nonexistent-pool")

			_, err := cognitoMock.AdminSetUserPassword(ctx, setPasswordInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user pool not found"))
		})

		It("should return injected error", func() {
			testError := errors.New("admin set user password error")
			cognitoMock.AdminSetUserPasswordError = testError

			_, err := cognitoMock.AdminSetUserPassword(ctx, setPasswordInput)
			Expect(err).To(Equal(testError))
		})
	})

	Describe("AdminUpdateUserAttributes", func() {
		var updateInput *cognitoidentityprovider.AdminUpdateUserAttributesInput

		BeforeEach(func() {
			cognitoMock.AddTestUserDirect(testUserPoolID, testUsername, testEmail, testPassword, true)

			updateInput = &cognitoidentityprovider.AdminUpdateUserAttributesInput{
				UserPoolId: aws.String(testUserPoolID),
				Username:   aws.String(testUsername),
				UserAttributes: []types.AttributeType{
					{
						Name:  aws.String("custom:user_id"),
						Value: aws.String("test-user-id-123"),
					},
				},
			}
		})

		It("should update user attributes successfully", func() {
			result, err := cognitoMock.AdminUpdateUserAttributes(ctx, updateInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify attribute was updated
			user := cognitoMock.GetUserByUsername(testUserPoolID, testUsername)
			Expect(user.Attributes["custom:user_id"]).To(Equal("test-user-id-123"))
		})

		It("should update multiple attributes", func() {
			updateInput.UserAttributes = append(updateInput.UserAttributes, types.AttributeType{
				Name:  aws.String("custom:another_field"),
				Value: aws.String("another-value"),
			})

			result, err := cognitoMock.AdminUpdateUserAttributes(ctx, updateInput)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify both attributes were updated
			user := cognitoMock.GetUserByUsername(testUserPoolID, testUsername)
			Expect(user.Attributes["custom:user_id"]).To(Equal("test-user-id-123"))
			Expect(user.Attributes["custom:another_field"]).To(Equal("another-value"))
		})

		It("should return error for non-existent user", func() {
			updateInput.Username = aws.String("nonexistent")

			_, err := cognitoMock.AdminUpdateUserAttributes(ctx, updateInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user not found"))
		})

		It("should return error for non-existent user pool", func() {
			updateInput.UserPoolId = aws.String("nonexistent-pool")

			_, err := cognitoMock.AdminUpdateUserAttributes(ctx, updateInput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user pool not found"))
		})

		It("should return injected error", func() {
			testError := errors.New("admin update user attributes error")
			cognitoMock.AdminUpdateUserAttributesError = testError

			_, err := cognitoMock.AdminUpdateUserAttributes(ctx, updateInput)
			Expect(err).To(Equal(testError))
		})
	})
})
