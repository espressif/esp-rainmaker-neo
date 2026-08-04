/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo, useState } from "react";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import OnboardingLayout from "@/containers/onboarding/onboarding-layout";
import { Button } from "@espressif/dashboard-ui-components/components";
import {
  Input,
  InputPassword,
} from "@espressif/dashboard-ui-components/components";
import {
  Checkbox,
  Label,
} from "@espressif/dashboard-ui-components/components";
import {
  Form,
  FormField,
  FormItem,
  FormControl,
  FormMessage,
} from "@espressif/dashboard-ui-components/components";
import { OnboardingCard } from "@/components/onboarding-card";
import { appConfig } from "@/lib/app-config";
import { ACCOUNT_SETTINGS_TABS_BY_ID } from "@/config/account-settings.config";
import {
  useSignin,
  getSigninRequestSchema,
  getAuthSchemaMessages,
  type SigninRequestSchema,
  resetLogoutFlag,
} from "@/api";
import {
  storeAuthTokens,
  storeKeepSignedIn,
  getKeepSignedIn,
  consumeRedirectPath,
} from "@/lib/auth";
import { useUserStore } from "@/stores/user.store";
import { ArrowRightIcon, LogIn } from "lucide-react";
import { Alert, Link } from "@espressif/dashboard-ui-components/components";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { parseLoginSearch } from "./_schema/login-search.schema";
import { voidFormSubmit } from "@/lib/void-form-submit";

export default function Login() {
  const { t } = useTranslation(["login", "common"]);
  const schema = useMemo(() => getSigninRequestSchema(getAuthSchemaMessages(t)), [t]);
  const navigate = useNavigate();
  const location = useLocation();
  const allowKeepMeSignedIn = appConfig.customAuth?.allowKeepMeSignedIn ?? false;
  // Prefilled from the last sign-in on this browser, so the choice does not have to be
  // made again every time. Only ever written on a successful submit.
  const [keepSignedIn, setKeepSignedIn] = useState(getKeepSignedIn);
  const [loginError, setLoginError] = useState<string | null>(null);
  const loginSearch = useMemo(
    () => parseLoginSearch(location.search),
    [location.search],
  );
  const passwordWasReset = loginSearch.reset === "success";

  const signinMutation = useSignin();
  const setLoggedInUserName = useUserStore((s) => s.setLoggedInUserName);

  const form = useForm<SigninRequestSchema>({
    resolver: zodResolver(schema),
    defaultValues: {
      username: "",
      password: "",
    },
  });

  const onSubmit = (data: SigninRequestSchema) => {
    setLoginError(null);
    signinMutation.mutate(data, {
      onSuccess: (response) => {
        // Backend returns 200 with only token_type for invalid credentials
        if (!response.access_token || !response.id_token) {
          signinMutation.reset();
          setLoginError(
            t("invalidCredentials", "Invalid username or password"),
          );
          return;
        }

        // The refresh token is persisted only on explicit opt-in: it is the one
        // credential that outlives the browser session, and it is what the background
        // session keeper needs to extend the session.
        storeAuthTokens({
          accessToken: response.access_token,
          idToken: response.id_token,
          refreshToken: keepSignedIn ? response.refresh_token : undefined,
        });
        storeKeepSignedIn(keepSignedIn);
        setLoggedInUserName(data.username);
        resetLogoutFlag();

        // Still on the shared bootstrap password — send them to change it before anything else.
        if (response.must_change_password) {
          void navigate({ to: ACCOUNT_SETTINGS_TABS_BY_ID.password.path });
          return;
        }

        // `?redirect=` wins: it is the destination the dead session handed over,
        // and it survives the hard navigation that `logout()` performs.
        const redirectPath = loginSearch.redirect ?? consumeRedirectPath();
        void navigate({ to: redirectPath || "/home" });
      },
    });
  };

  return (
    <OnboardingLayout>
      <OnboardingCard
        icon={<LogIn className="w-6 h-6" />}
        title={t("title", "Sign in")}
        description={t(
          "description",
          "Enter your credentials to access your account",
        )}
      >
        {(signinMutation.error || loginError) && (
          <Alert
            title={t("errorTitle", "Unable to login")}
            type="error"
            description={
              loginError ||
              signinMutation.error?.message ||
              t(
                "common:errorMessage",
                "An unexpected error occurred. Please try again later.",
              )
            }
            hideIcon
            className="border-none shadow-none mb-4"
          />
        )}

        {passwordWasReset && !loginError && !signinMutation.error && (
          <Alert
            title={t("resetSuccessTitle", "Password updated")}
            type="success"
            description={t(
              "resetSuccessMessage",
              "Your password has been reset. Sign in with your new password.",
            )}
            hideIcon
            className="border-none shadow-none mb-4"
          />
        )}

        <Form {...form}>
          <form onSubmit={voidFormSubmit(form.handleSubmit(onSubmit))} className="space-y-6">
            <FormField
              control={form.control}
              name="username"
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Input
                      type="text"
                      placeholder={t(
                        "usernamePlaceholder",
                        "Enter your username",
                      )}
                      label={t("usernameLabel", "Username")}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="password"
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <InputPassword
                      placeholder={t(
                        "passwordPlaceholder",
                        "Enter your password",
                      )}
                      autoComplete="current-password"
                      label={t("passwordLabel", "Password")}
                      hintContent={
                        <Link
                          to="/forgot-password"
                          linkComponent={TanstackRouterLink}
                          color="primary"
                          underline={false}
                        >
                          {t("forgotPasswordLink", "Forgot password?")}
                        </Link>
                      }
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            {allowKeepMeSignedIn && (
              <div className="flex items-center space-x-2">
                <Checkbox
                  id="keep-signed-in"
                  checked={keepSignedIn}
                  onCheckedChange={(checked) =>
                    setKeepSignedIn(checked === true)
                  }
                />
                <Label
                  htmlFor="keep-signed-in"
                  className="text-sm font-normal cursor-pointer"
                >
                  {t("keepSignedInLabel", "Keep me signed in")}
                </Label>
              </div>
            )}

            <Button
              type="submit"
              loading={signinMutation.isPending}
              size="lg"
              endIcon={<ArrowRightIcon className="w-4 h-4" />}
              animateEndIconOnHover={true}
              loadingIndicator="progress-bar"
            >
              {t("submit", "Sign in")}
            </Button>
          </form>
        </Form>
      </OnboardingCard>
    </OnboardingLayout>
  );
}
