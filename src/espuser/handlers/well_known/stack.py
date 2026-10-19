# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct

from app_common import (
    CommonResources,
    add_cors_options,
    create_base_lambda_role,
    create_cfn_api_method,
    create_lambda_function,
    get_or_create_api_resource,
)


class WellKnownAPI(Construct):
    """OIDC/OAuth discovery on the issuer's own host: GET /.well-known/openid-configuration,
    /.well-known/oauth-authorization-server and /.well-known/jwks.json, built per request from
    USER_ISSUER, ESPUSER_API_BASE and the JWKS in SSM. See espuser/docs/specs/oidc-oauth2.md.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        function_name = "well_known"
        well_known_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # USER_ISSUER, USER_JWKS_PARA_NAME and the read of that JWKS parameter come from create_lambda_function.
        self.well_known_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=well_known_lambda_role,
            environment={
                # The documents advertise the /oauth2/* endpoints at this base.
                "ESPUSER_API_BASE": common_resources.esp_user_api_url,
            },
        )

        # Public by specification (OIDC Discovery 1.0 s4, RFC 8414 s3). The helpers derive each logical ID from the URL path, so moving these to another construct never re-creates them.
        well_known_id = get_or_create_api_resource(
            self, "WellKnownResource", common_resources,
            common_resources.esp_user_api_root_resource_id, ".well-known",
            api_id=common_resources.esp_user_api_id
        )
        for verb_id, path_part in (("OpenIdConfiguration", "openid-configuration"),
                                   ("OAuthAuthorizationServer", "oauth-authorization-server"),
                                   ("Jwks", "jwks.json")):
            resource_id = get_or_create_api_resource(
                self, f"{verb_id}Resource", common_resources,
                well_known_id, path_part,
                api_id=common_resources.esp_user_api_id
            )
            create_cfn_api_method(
                self, f"{verb_id}GetMethod", common_resources,
                resource_id, "GET", self.well_known_function,
                authorization_type="NONE",
                api_id=common_resources.esp_user_api_id
            )
            add_cors_options(
                self, f"{verb_id}OptionsMethod", common_resources,
                resource_id, allowed_methods=["GET"],
                api_id=common_resources.esp_user_api_id
            )
