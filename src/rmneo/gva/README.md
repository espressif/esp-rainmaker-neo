# GVA (Google Virtual Assistant) Skill

This directory contains the implementation of the Google Assistant Smart Home integration for the RainMaker platform, similar to the Alexa skill implementation.

## Structure

The GVA skill is structured parallel to the Alexa skill:

```
src/user/gva_action/
├── README.md                    # This file
├── stack.py                     # CDK infrastructure stack
├── types.go                     # Google Assistant request/response types
├── utils.go                     # Utility functions (auth, device mapping)
├── handle_sync.go               # Device discovery handler
├── handle_query.go              # Device state query handler
├── handle_execute.go            # Device control handler
├── handle_disconnect.go         # Account unlinking handler
├── send_notification.go         # State change notifications
├── gva_action/
│   └── gva_action_main.go      # Main Lambda entry point
└── gva_cfg/
    ├── stack.py                 # Configuration API stack
    └── gva_cfg_main.go         # Configuration management
```

## Key Features

### 1. Smart Home API Support
- **SYNC**: Device discovery and capability reporting
- **QUERY**: Device state queries
- **EXECUTE**: Device control commands
- **DISCONNECT**: Account unlinking

### 2. Device Capabilities
The GVA skill maps RainMaker device parameters to Google Assistant traits:
- `OnOff` - Power control
- `Brightness` - Dimming control
- `ColorSetting` - Color/HSV control
- `FanSpeed` - Fan speed control
- `TemperatureSetting` - Thermostat control

### 3. Authentication & Authorization
- OAuth 2.0 flow with Google using Cognito endpoints directly
- JWT token validation using Cognito
- Per-user access control through groups

### 4. State Notifications
- Proactive state reporting to Google Assistant
- Real-time device state synchronization
- Support for test/mock environments

## Infrastructure

The GVA skill creates:
- Lambda function for handling Smart Home requests
- API Gateway for webhook endpoint
- IAM roles with appropriate permissions
- SSM parameters for client credentials
- Configuration API for credential management

## Integration Points

### With Node Management
- Device discovery from node configurations
- State updates via IoT shadows
- Group-based permissions

### With Notification System
- Registers as notification service
- Processes shadow update events
- Sends state reports to Google Assistant

## Configuration

The GVA skill requires:
1. Google Actions project with Smart Home Actions
2. OAuth 2.0 client credentials
3. Account linking configuration
4. Webhook fulfillment URL

## Testing

The implementation supports mock mode for testing without actual Google Assistant integration.

## Deployment

The GVA skill is deployed as part of the main RainMaker stack via CDK.