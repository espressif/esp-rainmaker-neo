# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# A Program called Dr X that is used to inspect and correct an rmng deployment
# For displaying logs: aws logs tail /aws/lambda/function_name --follow

import os
import sys
import json
import requests
import boto3
import argparse
import logging
from typing import Dict, List, Optional
from botocore.exceptions import ClientError

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class DrX:
    def __init__(self):
        region = os.environ.get('AWS_REGION')
        if not region:
            logger.warning("AWS_REGION environment variable not set. Using default region.")
        self.lambda_client = boto3.client('lambda', region_name=region)
        self.ssm_client = boto3.client('ssm', region_name=region)

    def list_functions(self) -> None:
        """List all Lambda functions in the deployment."""
        try:
            paginator = self.lambda_client.get_paginator('list_functions')
            for page in paginator.paginate():
                for function in page['Functions']:
                    print(f"Function: {function['FunctionName']}")
                    print(f"  {function['Runtime']} / {function['MemorySize']}MB / {function['Timeout']}s")

                    env_vars = function.get('Environment', {}).get('Variables', {})
                    print("  Environment Variables:") if env_vars else None
                    for key, value in env_vars.items():
                        print(f"    {key}: {value}")
                    print("-" * 80)
        except ClientError as e:
            logger.error(f"Error listing functions: {str(e)}")
            sys.exit(1)

    def update_function_env(self, function_name: str, env_updates: Dict[str, str]) -> None:
        """Update environment variables for a specific Lambda function.
        
        Args:
            function_name: Name of the Lambda function
            env_updates: Dictionary of environment variable name-value pairs to update
        """
        try:
            response = self.lambda_client.get_function_configuration(
                FunctionName=function_name
            )
            
            env_vars = response.get('Environment', {}).get('Variables', {})
            if not isinstance(env_vars, dict):
                env_vars = {}
            
            env_vars.update(env_updates)
            
            self.lambda_client.update_function_configuration(
                FunctionName=function_name,
                Environment={'Variables': env_vars}
            )
            
            logger.info(f"Successfully updated {len(env_updates)} environment variable(s) for function {function_name}")
            for var_name in env_updates.keys():
                logger.info(f"  - {var_name}")
        
        except ClientError as e:
            logger.error(f"Error updating function environment: {str(e)}")
            sys.exit(1)

    def set_log_config(self, function_name: str, log_level: Optional[str] = None, allow_dict: Optional[Dict[str, str]] = None, reset: bool = False) -> None:
        """Set logging configuration for a Lambda function.

        Dual-writes to SSM (persistent across deploys) and the Lambda env var
        (immediate effect on warm containers).

        Args:
            function_name: Name of the Lambda function
            log_level: Logging level (info, debug, trace, warn)
            allow_dict: Dictionary of key-value pairs for filtering
            reset: If True, removes RLOG configuration
        """
        try:
            ssm_param = f"/rmng/rlog/{function_name}"

            if reset:
                self._delete_ssm_parameter(ssm_param)
                self.update_function_env(function_name, {"RLOG": ""})
                logger.info(f"Reset RLOG configuration for function {function_name}")
                return

            if log_level is None:
                logger.error("Log level must be specified when not using --reset")
                sys.exit(1)

            rlog_config = {"level": log_level.lower()}
            if allow_dict:
                rlog_config["allow"] = allow_dict
            rlog_value = json.dumps(rlog_config)

            # SSM for persistence across deploys
            self._put_ssm_parameter(ssm_param, rlog_value)
            # Env var for immediate effect on warm containers
            self.update_function_env(function_name, {"RLOG": rlog_value})

            logger.info(f"Configured logging for function {function_name}: {rlog_value}")

        except ClientError as e:
            logger.error(f"Error setting log configuration: {str(e)}")
            sys.exit(1)

    def set_log_config_all(self, log_level: Optional[str] = None, allow_dict: Optional[Dict[str, str]] = None, reset: bool = False) -> None:
        """Set logging configuration for all Lambda functions.

        Writes to the global SSM parameter /rmng/rlog/global (persistent) and
        sets the env var on every Lambda (immediate effect).

        Args:
            log_level: Logging level (info, debug, trace, warn)
            allow_dict: Dictionary of key-value pairs for filtering
            reset: If True, removes RLOG configuration
        """
        try:
            ssm_param = "/rmng/rlog/global"

            if reset:
                self._delete_ssm_parameter(ssm_param)
            else:
                if log_level is None:
                    logger.error("Log level must be specified when not using --reset")
                    sys.exit(1)
                rlog_config = {"level": log_level.lower()}
                if allow_dict:
                    rlog_config["allow"] = allow_dict
                rlog_value = json.dumps(rlog_config)
                self._put_ssm_parameter(ssm_param, rlog_value)

            # Set env var on every Lambda for immediate effect
            paginator = self.lambda_client.get_paginator('list_functions')
            for page in paginator.paginate():
                for function in page['Functions']:
                    function_name = function['FunctionName']
                    try:
                        if reset:
                            self.update_function_env(function_name, {"RLOG": ""})
                        else:
                            self.update_function_env(function_name, {"RLOG": rlog_value})
                    except Exception as e:
                        logger.error(f"Failed to update env for function {function_name}: {str(e)}")
                        continue

            action = "Reset" if reset else "Configured"
            logger.info(f"{action} logging for all functions (SSM: {ssm_param})")

        except ClientError as e:
            logger.error(f"Error setting log configuration: {str(e)}")
            sys.exit(1)

    def get_log_config(self, function_name: Optional[str] = None, show_all: bool = False) -> None:
        """Display persisted RLOG configuration from SSM.

        Args:
            function_name: Show per-function config for this Lambda
            show_all: Show the global config
        """
        try:
            if function_name:
                param_name = f"/rmng/rlog/{function_name}"
                value = self._get_ssm_parameter(param_name)
                if value:
                    print(f"Per-function ({function_name}): {value}")
                else:
                    print(f"No per-function config for {function_name}")

            if show_all or not function_name:
                value = self._get_ssm_parameter("/rmng/rlog/global")
                if value:
                    print(f"Global: {value}")
                else:
                    print("No global config")
        except ClientError as e:
            logger.error(f"Error reading log configuration: {str(e)}")
            sys.exit(1)

    def _put_ssm_parameter(self, name: str, value: str) -> None:
        self.ssm_client.put_parameter(
            Name=name,
            Value=value,
            Type='String',
            Overwrite=True,
        )
        logger.info(f"SSM parameter {name} set")

    def _get_ssm_parameter(self, name: str) -> Optional[str]:
        try:
            result = self.ssm_client.get_parameter(Name=name)
            return result['Parameter']['Value']
        except self.ssm_client.exceptions.ParameterNotFound:
            return None

    def _delete_ssm_parameter(self, name: str) -> None:
        try:
            self.ssm_client.delete_parameter(Name=name)
            logger.info(f"SSM parameter {name} deleted")
        except self.ssm_client.exceptions.ParameterNotFound:
            logger.info(f"SSM parameter {name} not found (already deleted)")

def main():
    parser = argparse.ArgumentParser(description='Dr X - RMNG Management Tool')
    subparsers = parser.add_subparsers(dest='command', help='Commands')

    # List functions command
    list_parser = subparsers.add_parser('list', help='List all Lambda functions')

    # Update environment variable command
    update_parser = subparsers.add_parser('update-env', help='Update Lambda function environment variables')
    update_parser.add_argument('function_name', help='Name of the Lambda function')
    update_parser.add_argument('env_vars', nargs='+', metavar='VAR_NAME=VAR_VALUE',
                               help='Environment variables in VAR_NAME=VAR_VALUE format. Multiple pairs can be specified.')

    # Set log configuration command
    log_parser = subparsers.add_parser('set-log', help='Set logging configuration for a Lambda function or all functions')
    # First define the positional arguments
    log_parser.add_argument('log_level', nargs='?', choices=['trace', 'debug', 'info', 'warn'],
                           help='Logging level (required unless --reset is specified)')
    # Then define the optional arguments
    log_parser.add_argument('--function', metavar='FUNCTION_NAME',
                           help='Name of the Lambda function to configure')
    log_parser.add_argument('--all', action='store_true', help='Apply configuration to all Lambda functions')
    log_parser.add_argument('--allow', nargs='+', metavar='KEY=VALUE',
                           help='Allow filters in key=value format. Multiple filters can be specified.')
    log_parser.add_argument('--user', metavar='USERNAME',
                           help='Username to filter logs for (adds uid filter)')
    log_parser.add_argument('--node', metavar='NODEID',
                           help='Node ID to filter logs for (adds nid filter)')
    log_parser.add_argument('--reset', action='store_true',
                           help='Remove the RLOG configuration from the function')

    # Get log configuration command
    get_log_parser = subparsers.add_parser('get-log', help='Show persisted RLOG configuration from SSM')
    get_log_parser.add_argument('--function', metavar='FUNCTION_NAME',
                                help='Show per-function config for this Lambda')
    get_log_parser.add_argument('--all', action='store_true',
                                help='Show the global config')

    args = parser.parse_args()
    
    drx = DrX()

    if args.command == 'list':
        drx.list_functions()
    elif args.command == 'update-env':
        env_updates = {}
        for env_pair in args.env_vars:
            try:
                var_name, var_value = env_pair.split('=', 1)
                env_updates[var_name.strip()] = var_value
            except ValueError:
                logger.error(f"Invalid environment variable format: {env_pair}. Expected format: VAR_NAME=VAR_VALUE")
                sys.exit(1)
        
        if not env_updates:
            logger.error("No valid environment variables provided")
            sys.exit(1)
        
        drx.update_function_env(args.function_name, env_updates)
    elif args.command == 'set-log':
        if not args.log_level and not args.reset:
            log_parser.error('log_level is required unless --reset is specified')
        
        if not args.all and not args.function:
            log_parser.error('--function or --all must be specified')
            sys.exit(1)
        
        # Build allow_dict from arguments
        allow_dict = {}
        if args.user:
            allow_dict["uid"] = args.user
        if args.node:
            allow_dict["nid"] = args.node
        if args.allow:
            for filter_pair in args.allow:
                try:
                    key, value = filter_pair.split('=')
                    allow_dict[key.strip()] = value.strip()
                except ValueError:
                    logger.error(f"Invalid filter format: {filter_pair}. Expected format: key=value")
                    sys.exit(1)
        
        if args.all:
            drx.set_log_config_all(args.log_level, allow_dict if allow_dict else None, args.reset)
        else:
            drx.set_log_config(args.function, args.log_level, allow_dict if allow_dict else None, args.reset)
    elif args.command == 'get-log':
        if not args.function and not args.all:
            get_log_parser.error('--function or --all must be specified')
        drx.get_log_config(function_name=args.function, show_all=args.all)
    else:
        parser.print_help()
        sys.exit(1)

if __name__ == '__main__':
    main()
