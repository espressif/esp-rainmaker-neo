# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus admin guide <topic>` — the console-side half of each integration setup.

These print what an operator must do in somebody else's console, filled in with this deployment's
ARNs and URLs. Under `admin` because each one ends in an admin command, not because it needs an
identity or credentials: it reads the outputs and prints text.
"""

import click

from ..outputs import oidc_endpoints
from . import output
from .context import Session

VA_CLIENT_ID = 'va-client'
VA_SECRET_HINT = ('the va-client secret from GET /v1/admin/clients?get_secret=true '
                  '(or SSM /espuser/base/va-client-secret)')


def _oidc(settings):
    """Authorize and token endpoints, as placeholders when the outputs carry none, so the printed
    instructions still show what is missing."""
    authorize, token = oidc_endpoints(settings.raw)
    return (authorize or '<EspUserApiUrl>/oauth2/authorize',
            token or '<EspUserApiUrl>/oauth2/token')


def print_alexa_instructions(settings):
    regions = settings.alexa_region_arns
    default_arn = settings.default_alexa_arn or '<AlexaSkillFunctionArn>'
    authorize_url, token_url = _oidc(settings)
    output.plain('')
    output.plain('Alexa skill configuration (https://developer.amazon.com/alexa/console/ask):')
    output.plain('1. On the skill\'s "Smart Home" page:')
    output.plain(f'   - Set the Default endpoint to {default_arn}')
    if not regions:
        output.plain(f'   - No AlexaSkillFunctionArn in {settings.source}; deploy the '
                     'rmng-alexa-core stack(s) first')
    else:
        output.plain('   - Set each geography:')
        for code in ('NA', 'EU', 'FE'):
            if code in regions:
                output.plain(f'     {code}: {regions[code]}')
    output.plain('2. On the Account Linking page:')
    output.plain(f'   - Your Web Authorization URL: {authorize_url}')
    output.plain(f'   - Access Token URI: {token_url}')
    output.plain(f'   - Your Client Id: {VA_CLIENT_ID}')
    output.plain(f'   - Your Secret: {VA_SECRET_HINT}')
    output.plain('   - Scope: add openid email phone profile')
    output.plain('3. Store the credentials: morpheus admin integrations alexa setup '
                 '<config_file>')


def print_smartthings_instructions(settings):
    regions = settings.st_region_arns
    authorize_url, token_url = _oidc(settings)
    output.plain('')
    output.plain('SmartThings configuration (https://developer.smartthings.com/):')
    output.plain('1. Device Integrations > create a Product > add a Cloud Connector > ST Schema')
    output.plain('   - The Product is the container; adding the Schema App links the two')
    output.plain('   - App icon: the logo at assets/smartthings_logo.png')
    output.plain('2. Set the Target ARN for each geography:')
    if not regions:
        output.plain(f'   - No STSchemaAppFunctionArn in {settings.source}; deploy the '
                     'rmng-st-core stack(s) first')
    else:
        for code, label in (('NA', 'North America'), ('EU', 'Europe'), ('AP', 'Asia-Pacific')):
            if code in regions:
                output.plain(f'     {label}: {regions[code]}')
    output.plain('3. Device Cloud Credentials (your cloud, given to SmartThings):')
    output.plain(f'   - Client ID: {VA_CLIENT_ID}')
    output.plain(f'   - Client Secret: {VA_SECRET_HINT}')
    output.plain(f'   - OAuth URL: {authorize_url}')
    output.plain(f'   - Token URL: {token_url}')
    output.plain('   - OAuth Scope: openid email phone profile')
    output.plain('4. Save. SmartThings then issues its OWN Client ID and Secret. Note the '
                 'direction: these differ from the credentials in step 3.')
    output.plain('5. Store them: morpheus admin integrations smartthings setup '
                 '<config_file>, where the file holds')
    output.plain('   {"client_id": "<SmartThings client id>", '
                 '"client_secret": "<SmartThings client secret>"}')
    output.plain('6. Link your account in the SmartThings app and the devices appear. Discovery '
                 'names a pre-made c2c-* handler type, so nothing per-device-type is created.')


def print_gva_instructions(settings):
    authorize_url, token_url = _oidc(settings)
    fulfillment_url = settings.gva_fulfillment_url or '<GVAFulfillmentUrl>'
    output.plain('')
    output.plain('Google Home configuration (https://console.home.google.com/projects):')
    output.plain('1. Create or open a Google Home project linked to your Google Cloud project')
    output.plain('2. Add a Cloud-to-cloud integration and configure under Develop > Setup:')
    output.plain(f'   - OAuth Client ID: {VA_CLIENT_ID}')
    output.plain(f'   - OAuth Client Secret: {VA_SECRET_HINT}')
    output.plain(f'   - Authorization URL: {authorize_url}')
    output.plain(f'   - Token URL: {token_url}')
    output.plain(f'   - Fulfillment URL: {fulfillment_url}')
    output.plain('   - Scopes: openid, email, phone, profile')
    output.plain('   - App icon: the logo at assets/gva_logo.png')
    output.plain('3. Enable the HomeGraph API at')
    output.plain('   https://console.cloud.google.com/apis/library/homegraph.googleapis.com')
    output.plain('4. Create a service account for Report State:')
    output.plain('   - IAM & Admin > Service Accounts > Create Service Account')
    output.plain('   - Role: Service Account OpenID Connect Identity Token Creator')
    output.plain('   - Keys > Add Key > Create New Key > JSON, and download it')
    output.plain('5. Store it: morpheus admin integrations gva setup '
                 '<service_account.json>')


def print_ios_instructions(settings):
    output.plain('')
    output.plain('Apple Push Notifications setup (https://developer.apple.com/account/resources):')
    output.plain('1. Create an App ID of type App:')
    output.plain('   - Add the bundle ID (used as <bundle_id>)')
    output.plain('   - Enable the Push Notifications capability')
    output.plain('   - Note the Team ID (used as <team_id>)')
    output.plain('2. Create a Key:')
    output.plain('   - Type: Apple Push Notifications service (APNs)')
    output.plain('   - Point it at the App ID created above')
    output.plain('   - Download the .p8 key file and note the Key ID (used as <key_id>)')
    output.plain('3. Register it: morpheus admin platforms register-ios '
                 '<p8_key_file> <key_id> <team_id> <bundle_id> [--sandbox]')


def print_android_instructions(settings):
    output.plain('')
    output.plain('Firebase Cloud Messaging setup (https://console.firebase.google.com/):')
    output.plain('1. Create or choose a project')
    output.plain('2. Go to Settings > Service accounts')
    output.plain("3. On the Firebase Admin SDK tab, click Generate new private key and download it")
    output.plain('4. Register it: morpheus admin platforms register-android '
                 '<service_account.json>')


TOPICS = {
    'alexa': print_alexa_instructions,
    'gva': print_gva_instructions,
    'smartthings': print_smartthings_instructions,
    'ios': print_ios_instructions,
    'android': print_android_instructions,
}


@click.command('guide')
@click.argument('topic', type=click.Choice(sorted(TOPICS)))
@click.pass_context
def guide(ctx, topic):
    """Print the console setup steps for TOPIC, filled in for this deployment."""
    TOPICS[topic](ctx.find_object(Session).settings)
