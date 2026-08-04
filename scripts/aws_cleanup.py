#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import boto3
import click
from rich.console import Console
from rich.progress import Progress
from botocore.exceptions import ClientError
import time

console = Console()

class AWSDeployCleanup:
    def __init__(self, profile=None, access_key=None, secret_key=None, session_token=None, region=None, services=None):
        """Initialize AWS session with provided credentials"""
        session_kwargs = {}
        if profile:
            session_kwargs['profile_name'] = profile
        if region:
            session_kwargs['region_name'] = region
        if access_key and secret_key:
            session_kwargs['aws_access_key_id'] = access_key
            session_kwargs['aws_secret_access_key'] = secret_key
            if session_token:
                session_kwargs['aws_session_token'] = session_token

        self.session = boto3.Session(**session_kwargs)
        self.console = Console()
        self.iam = self.session.client('iam')
        self.lambda_client = self.session.client('lambda')
        self.s3 = self.session.client('s3')
        self.cloudformation = self.session.client('cloudformation')
        self.cognito = self.session.client('cognito-idp')
        self.dynamodb = self.session.client('dynamodb')
        self.iot = self.session.client('iot')
        self.cloudwatch = self.session.client('logs')
        self.cloudfront = self.session.client('cloudfront')
        self.sns = self.session.client('sns')
        self.services = services if services else ['all']

    def cleanup_cognito(self):
        """Clean up Cognito user pools"""
        try:
            # List all user pools
            self.console.print("[bold cyan]Listing Cognito user pools...[/bold cyan]")
            pools = []
            paginator = self.cognito.get_paginator('list_user_pools')
            
            for page in paginator.paginate(MaxResults=60):
                for pool in page.get('UserPools', []):
                    pools.append(pool)
                    self.console.print(f"Found user pool: {pool.get('Name')}")

            if not pools:
                self.console.print("[yellow]No user pools found[/yellow]")
                return

            # Delete each user pool
            self.console.print(f"\n[bold cyan]Found {len(pools)} user pools to delete[/bold cyan]")
            for pool in pools:
                pool_id = pool.get('Id')
                pool_name = pool.get('Name')
                try:
                    self.console.print(f"\n[bold]Processing user pool: {pool_name}[/bold]")
                    
                    # First, check if the user pool has a domain and delete it
                    try:
                        self.console.print("\n[cyan]Checking for user pool domain...[/cyan]")
                        # Get user pool details to check for domain
                        pool_details = self.cognito.describe_user_pool(UserPoolId=pool_id)
                        domain_info = pool_details.get('UserPool', {}).get('Domain')
                        
                        if domain_info:
                            domain_name = domain_info
                            self.console.print(f"Found domain: {domain_name}")
                            self.console.print("\n[cyan]Deleting user pool domain...[/cyan]")
                            self.cognito.delete_user_pool_domain(Domain=domain_name, UserPoolId=pool_id)
                            self.console.print(f"[green]✓ Successfully deleted domain: {domain_name}[/green]")
                        else:
                            self.console.print("No domain configured for this user pool.")
                            
                    except ClientError as e:
                        error_code = e.response['Error']['Code']
                        if error_code == 'ResourceNotFoundException':
                            self.console.print("No domain configured for this user pool.")
                        else:
                            self.console.print(f"[yellow]⚠ Warning: Could not check/delete domain: {str(e)}[/yellow]")
                    
                    # Delete the user pool
                    self.console.print("\n[cyan]Deleting user pool...[/cyan]")
                    self.cognito.delete_user_pool(UserPoolId=pool_id)
                    self.console.print(f"[green]✓ Successfully deleted user pool: {pool_name}[/green]")
                    
                except ClientError as e:
                    error_code = e.response['Error']['Code']
                    error_message = e.response['Error']['Message']
                    self.console.print(f"[red]✗ Error deleting user pool {pool_name}: {error_code} - {error_message}[/red]")
                    continue

            self.console.print("\n[bold green]Cognito cleanup completed![/bold green]")

        except ClientError as e:
            error_code = e.response['Error']['Code']
            error_message = e.response['Error']['Message']
            self.console.print(f"[red]Error cleaning up Cognito user pools: {error_code} - {error_message}[/red]")
            raise
        except Exception as e:
            self.console.print(f"[red]Error cleaning up Cognito user pools: {str(e)}[/red]")
            raise

    def cleanup_dynamodb(self):
        """Clean up DynamoDB tables"""
        try:
            # List all tables
            self.console.print("[bold cyan]Listing DynamoDB tables...[/bold cyan]")
            tables = []
            paginator = self.dynamodb.get_paginator('list_tables')
            
            for page in paginator.paginate():
                for table_name in page.get('TableNames', []):
                    tables.append(table_name)
                    self.console.print(f"Found table: {table_name}")

            if not tables:
                self.console.print("[yellow]No tables found[/yellow]")
                return

            # Delete each table
            self.console.print(f"\n[bold cyan]Found {len(tables)} tables to delete[/bold cyan]")
            for table_name in tables:
                try:
                    self.console.print(f"\n[bold]Processing table: {table_name}[/bold]")
                    
                    # Delete the table
                    self.console.print("\n[cyan]Deleting table...[/cyan]")
                    self.dynamodb.delete_table(TableName=table_name)
                    
                    # Wait for table deletion to complete
                    self.console.print("[cyan]Waiting for table deletion to complete...[/cyan]")
                    waiter = self.dynamodb.get_waiter('table_not_exists')
                    waiter.wait(TableName=table_name)
                    
                    self.console.print(f"[green]✓ Successfully deleted table: {table_name}[/green]")
                    
                except ClientError as e:
                    error_code = e.response['Error']['Code']
                    error_message = e.response['Error']['Message']
                    self.console.print(f"[red]✗ Error deleting table {table_name}: {error_code} - {error_message}[/red]")
                    continue

            self.console.print("\n[bold green]DynamoDB cleanup completed![/bold green]")

        except ClientError as e:
            error_code = e.response['Error']['Code']
            error_message = e.response['Error']['Message']
            self.console.print(f"[red]Error cleaning up DynamoDB tables: {error_code} - {error_message}[/red]")
            raise
        except Exception as e:
            self.console.print(f"[red]Error cleaning up DynamoDB tables: {str(e)}[/red]")
            raise

    def cleanup_iot(self):
        """Clean up IoT resources"""
        try:
            # First, delete all things
            self.console.print("[bold cyan]Listing IoT things...[/bold cyan]")
            things = []
            paginator = self.iot.get_paginator('list_things')
            
            for page in paginator.paginate():
                for thing in page.get('things', []):
                    things.append(thing)
                    self.console.print(f"Found thing: {thing.get('thingName')}")

            # Delete each thing
            if things:
                self.console.print(f"\n[bold cyan]Found {len(things)} things to delete[/bold cyan]")
                for thing in things:
                    thing_name = thing.get('thingName')
                    try:
                        self.console.print(f"\n[bold]Processing thing: {thing_name}[/bold]")
                        
                        # First detach any certificates from the thing
                        self.console.print("\n[cyan]Detaching certificates from thing...[/cyan]")
                        try:
                            principals = self.iot.list_thing_principals(thingName=thing_name)
                            for principal in principals.get('principals', []):
                                try:
                                    self.iot.detach_thing_principal(
                                        thingName=thing_name,
                                        principal=principal
                                    )
                                    self.console.print(f"[green]✓ Detached certificate: {principal}[/green]")
                                except ClientError as e:
                                    self.console.print(f"[yellow]⚠ Warning: Could not detach certificate {principal}: {str(e)}[/yellow]")
                        except ClientError as e:
                            self.console.print(f"[yellow]⚠ Warning: Could not list certificates for thing: {str(e)}[/yellow]")

                        # Then delete the thing
                        self.console.print("\n[cyan]Deleting thing...[/cyan]")
                        self.iot.delete_thing(thingName=thing_name)
                        self.console.print(f"[green]✓ Successfully deleted thing: {thing_name}[/green]")
                        
                    except ClientError as e:
                        error_code = e.response['Error']['Code']
                        error_message = e.response['Error']['Message']
                        self.console.print(f"[red]✗ Error deleting thing {thing_name}: {error_code} - {error_message}[/red]")
                        continue

            # Then, delete all certificates
            self.console.print("\n[bold cyan]Listing IoT certificates...[/bold cyan]")
            certificates = []
            paginator = self.iot.get_paginator('list_certificates')
            
            for page in paginator.paginate():
                for cert in page.get('certificates', []):
                    certificates.append(cert)
                    self.console.print(f"Found certificate: {cert.get('certificateId')}")

            # Delete each certificate
            if certificates:
                self.console.print(f"\n[bold cyan]Found {len(certificates)} certificates to delete[/bold cyan]")
                for cert in certificates:
                    cert_id = cert.get('certificateId')
                    try:
                        self.console.print(f"\n[bold]Processing certificate: {cert_id}[/bold]")
                        
                        # First detach all policies from the certificate
                        self.console.print("\n[cyan]Detaching policies from certificate...[/cyan]")
                        try:
                            policies = self.iot.list_principal_policies(principal=cert.get('certificateArn'))
                            for policy in policies.get('policies', []):
                                try:
                                    self.iot.detach_policy(
                                        policyName=policy.get('policyName'),
                                        target=cert.get('certificateArn')
                                    )
                                    self.console.print(f"[green]✓ Detached policy: {policy.get('policyName')}[/green]")
                                except ClientError as e:
                                    self.console.print(f"[yellow]⚠ Warning: Could not detach policy {policy.get('policyName')}: {str(e)}[/yellow]")
                        except ClientError as e:
                            self.console.print(f"[yellow]⚠ Warning: Could not list policies for certificate: {str(e)}[/yellow]")

                        # Then delete the certificate
                        self.console.print("\n[cyan]Deleting certificate...[/cyan]")
                        self.iot.update_certificate(
                            certificateId=cert_id,
                            newStatus='INACTIVE'
                        )
                        self.iot.delete_certificate(certificateId=cert_id, forceDelete=True)
                        self.console.print(f"[green]✓ Successfully deleted certificate: {cert_id}[/green]")
                        
                    except ClientError as e:
                        error_code = e.response['Error']['Code']
                        error_message = e.response['Error']['Message']
                        self.console.print(f"[red]✗ Error deleting certificate {cert_id}: {error_code} - {error_message}[/red]")
                        continue

            # Finally, delete all policies
            self.console.print("\n[bold cyan]Listing IoT policies...[/bold cyan]")
            policies = []
            paginator = self.iot.get_paginator('list_policies')
            
            for page in paginator.paginate():
                for policy in page.get('policies', []):
                    policies.append(policy)
                    self.console.print(f"Found policy: {policy.get('policyName')}")

            # Delete each policy
            if policies:
                self.console.print(f"\n[bold cyan]Found {len(policies)} policies to delete[/bold cyan]")
                for policy in policies:
                    policy_name = policy.get('policyName')
                    try:
                        self.console.print(f"\n[bold]Processing policy: {policy_name}[/bold]")
                        
                        # First, detach the policy from all principals (targets)
                        self.console.print("\n[cyan]Detaching policy from all principals...[/cyan]")
                        try:
                            paginator = self.iot.get_paginator('list_targets_for_policy')
                            targets = []
                            for page in paginator.paginate(policyName=policy_name):
                                for target in page.get('targets', []):
                                    targets.append(target)
                            
                            if targets:
                                self.console.print(f"Found {len(targets)} principals attached to policy")
                                for target in targets:
                                    try:
                                        self.iot.detach_policy(
                                            policyName=policy_name,
                                            target=target
                                        )
                                        self.console.print(f"[green]✓ Detached policy from: {target}[/green]")
                                    except ClientError as e:
                                        error_code = e.response['Error']['Code']
                                        if error_code != 'ResourceNotFoundException':
                                            self.console.print(f"[yellow]⚠ Warning: Could not detach policy from {target}: {str(e)}[/yellow]")
                            else:
                                self.console.print("No principals attached to this policy")
                        except ClientError as e:
                            error_code = e.response['Error']['Code']
                            if error_code == 'ResourceNotFoundException':
                                self.console.print("Policy not found, skipping")
                                continue
                            else:
                                self.console.print(f"[yellow]⚠ Warning: Could not list targets for policy: {str(e)}[/yellow]")
                        
                        # Delete the policy
                        self.console.print("\n[cyan]Deleting policy...[/cyan]")
                        self.iot.delete_policy(policyName=policy_name)
                        self.console.print(f"[green]✓ Successfully deleted policy: {policy_name}[/green]")
                        
                    except ClientError as e:
                        error_code = e.response['Error']['Code']
                        error_message = e.response['Error']['Message']
                        self.console.print(f"[red]✗ Error deleting policy {policy_name}: {error_code} - {error_message}[/red]")
                        continue

            self.console.print("\n[bold green]IoT cleanup completed![/bold green]")

        except ClientError as e:
            error_code = e.response['Error']['Code']
            error_message = e.response['Error']['Message']
            self.console.print(f"[red]Error cleaning up IoT resources: {error_code} - {error_message}[/red]")
            raise
        except Exception as e:
            self.console.print(f"[red]Error cleaning up IoT resources: {str(e)}[/red]")
            raise

    def cleanup_cloudwatch(self):
        """Clean up CloudWatch resources"""
        try:
            # List all log groups
            self.console.print("[bold cyan]Listing CloudWatch log groups...[/bold cyan]")
            log_groups = []
            
            try:
                # Use describe_log_groups directly instead of paginator
                response = self.cloudwatch.describe_log_groups()
                for group in response.get('logGroups', []):
                    log_groups.append(group)
                    self.console.print(f"Found log group: {group.get('logGroupName')}")
                
                # Handle pagination manually if there are more results
                while 'nextToken' in response:
                    response = self.cloudwatch.describe_log_groups(nextToken=response['nextToken'])
                    for group in response.get('logGroups', []):
                        log_groups.append(group)
                        self.console.print(f"Found log group: {group.get('logGroupName')}")
                            
            except ClientError as e:
                error_code = e.response['Error']['Code']
                error_message = e.response['Error']['Message']
                self.console.print(f"[red]Error listing log groups: {error_code} - {error_message}[/red]")
                self.console.print("[yellow]Please ensure you have the following permissions:[/yellow]")
                self.console.print("- logs:DescribeLogGroups")
                self.console.print("- logs:DeleteLogGroup")
                raise

            if not log_groups:
                self.console.print("[yellow]No log groups found[/yellow]")
                return

            # Delete each log group
            self.console.print(f"\n[bold cyan]Found {len(log_groups)} log groups to delete[/bold cyan]")
            for group in log_groups:
                group_name = group.get('logGroupName')
                try:
                    self.console.print(f"\n[bold]Processing log group: {group_name}[/bold]")
                    
                    # Delete the log group
                    self.console.print("\n[cyan]Deleting log group...[/cyan]")
                    self.cloudwatch.delete_log_group(logGroupName=group_name)
                    self.console.print(f"[green]✓ Successfully deleted log group: {group_name}[/green]")
                    
                except ClientError as e:
                    error_code = e.response['Error']['Code']
                    error_message = e.response['Error']['Message']
                    self.console.print(f"[red]✗ Error deleting log group {group_name}: {error_code} - {error_message}[/red]")
                    continue

            self.console.print("\n[bold green]CloudWatch cleanup completed![/bold green]")

        except ClientError as e:
            error_code = e.response['Error']['Code']
            error_message = e.response['Error']['Message']
            self.console.print(f"[red]Error cleaning up CloudWatch resources: {error_code} - {error_message}[/red]")
            raise
        except Exception as e:
            self.console.print(f"[red]Error cleaning up CloudWatch resources: {str(e)}[/red]")
            raise

    def cleanup_lambda_functions(self):
        """Clean up Lambda functions"""
        try:
            # List all functions
            self.console.print("[bold cyan]Listing Lambda functions...[/bold cyan]")
            functions = []
            paginator = self.lambda_client.get_paginator('list_functions')
            
            for page in paginator.paginate():
                for function in page.get('Functions', []):
                    functions.append(function)
                    self.console.print(f"Found function: {function.get('FunctionName')}")

            if not functions:
                self.console.print("[yellow]No functions found[/yellow]")
                return

            # Delete each function
            self.console.print(f"\n[bold cyan]Found {len(functions)} functions to delete[/bold cyan]")
            for function in functions:
                function_name = function.get('FunctionName')
                try:
                    self.console.print(f"\n[bold]Processing function: {function_name}[/bold]")
                    
                    # Delete the function
                    self.console.print("\n[cyan]Deleting function...[/cyan]")
                    self.lambda_client.delete_function(FunctionName=function_name)
                    self.console.print(f"[green]✓ Successfully deleted function: {function_name}[/green]")
                    
                except ClientError as e:
                    error_code = e.response['Error']['Code']
                    error_message = e.response['Error']['Message']
                    self.console.print(f"[red]✗ Error deleting function {function_name}: {error_code} - {error_message}[/red]")
                    continue

            self.console.print("\n[bold green]Lambda cleanup completed![/bold green]")

        except ClientError as e:
            error_code = e.response['Error']['Code']
            error_message = e.response['Error']['Message']
            self.console.print(f"[red]Error cleaning up Lambda functions: {error_code} - {error_message}[/red]")
            raise
        except Exception as e:
            self.console.print(f"[red]Error cleaning up Lambda functions: {str(e)}[/red]")
            raise

    def cleanup_cloudformation_stacks(self):
        """Clean up CloudFormation stacks"""
        try:
            # List all stacks
            self.console.print("[bold cyan]Listing CloudFormation stacks...[/bold cyan]")
            stacks = []
            paginator = self.cloudformation.get_paginator('list_stacks')
            
            for page in paginator.paginate(StackStatusFilter=['CREATE_COMPLETE', 'UPDATE_COMPLETE', 'UPDATE_ROLLBACK_COMPLETE', 'ROLLBACK_COMPLETE', 'DELETE_FAILED']):
                for stack in page.get('StackSummaries', []):
                    # Filter out deleted and nested stacks
                    if (stack['StackStatus'] != 'DELETE_COMPLETE' and 
                        'ParentId' not in stack):
                        stacks.append(stack)
                        self.console.print(f"Found stack: {stack.get('StackName')}")

            if not stacks:
                self.console.print("[yellow]No stacks found[/yellow]")
                return

            # Sort stacks by creation time (newest first)
            self.console.print(f"\n[bold cyan]Found {len(stacks)} stacks to delete[/bold cyan]")
            sorted_stacks = sorted(stacks, key=lambda x: x.get('CreationTime', ''), reverse=True)
            
            for stack in sorted_stacks:
                stack_name = stack.get('StackName')
                creation_time = stack.get('CreationTime', '').strftime('%Y-%m-%d %H:%M:%S')
                try:
                    self.console.print(f"\n[bold]Processing stack: {stack_name} (Created: {creation_time})[/bold]")
                    
                    # Describe the stack to check its status
                    response = self.cloudformation.describe_stacks(StackName=stack_name)
                    stack_status = response['Stacks'][0]['StackStatus']
                    self.console.print(f"Current status of stack '{stack_name}': {stack_status}")

                    # Try to disable termination protection
                    try:
                        self.console.print(f"Disabling termination protection for stack '{stack_name}'...")
                        self.cloudformation.update_termination_protection(
                            StackName=stack_name,
                            EnableTerminationProtection=False
                        )
                    except ClientError as e:
                        if "Stack does not have termination protection enabled" in str(e):
                            self.console.print("Termination protection was already disabled.")
                        else:
                            self.console.print(f"Error disabling termination protection: {e}")

                    # Check if the stack can be deleted
                    if stack_status in ['CREATE_COMPLETE', 'UPDATE_COMPLETE', 'ROLLBACK_COMPLETE', 'DELETE_FAILED', 'UPDATE_ROLLBACK_COMPLETE']:
                        try:
                            self.console.print(f"Deleting stack '{stack_name}'...")
                            self.cloudformation.delete_stack(StackName=stack_name)
                            waiter = self.cloudformation.get_waiter('stack_delete_complete')
                            waiter.wait(
                                StackName=stack_name,
                                WaiterConfig={'Delay': 30, 'MaxAttempts': 240}  # 30 seconds * 240 attempts = 2 hours
                            )
                        except ClientError as e:
                            self.console.print(f"Regular deletion failed, attempting force delete...")
                            self.cloudformation.delete_stack(StackName=stack_name, DeletionMode='FORCE_DELETE_STACK')
                            waiter = self.cloudformation.get_waiter('stack_delete_complete')
                            waiter.wait(
                                StackName=stack_name,
                                WaiterConfig={'Delay': 30, 'MaxAttempts': 240}  # 30 seconds * 240 attempts = 2 hours
                            )
                        
                        self.console.print(f"[green]✓ Successfully deleted stack: {stack_name}[/green]")
                    else:
                        self.console.print(f"[yellow]Stack '{stack_name}' cannot be deleted because its current status is '{stack_status}'.[/yellow]")
                        continue
                    
                except ClientError as e:
                    error_code = e.response['Error']['Code']
                    error_message = e.response['Error']['Message']
                    if error_code == 'ValidationError':
                        self.console.print(f"[yellow]Stack '{stack_name}' does not exist, skipping.[/yellow]")
                    else:
                        self.console.print(f"[red]✗ Error deleting stack {stack_name}: {error_code} - {error_message}[/red]")
                    continue

            self.console.print("\n[bold green]CloudFormation cleanup completed![/bold green]")

        except ClientError as e:
            error_code = e.response['Error']['Code']
            error_message = e.response['Error']['Message']
            self.console.print(f"[red]Error cleaning up CloudFormation stacks: {error_code} - {error_message}[/red]")
            raise
        except Exception as e:
            self.console.print(f"[red]Error cleaning up CloudFormation stacks: {str(e)}[/red]")
            raise

    def cleanup_s3_buckets(self):
        """Clean up S3 buckets except those with specific prefixes"""
        try:
            # List all buckets
            self.console.print("[bold cyan]Listing S3 buckets...[/bold cyan]")
            buckets = []
            response = self.s3.list_buckets()
            
            for bucket in response.get('Buckets', []):
                bucket_name = bucket.get('Name')
                # Skip buckets with specific prefixes
                if (bucket_name.startswith('esp-rainmaker-sam-deployments-') or 
                    bucket_name.startswith('publish-')):
                    self.console.print(f"[yellow]Skipping protected bucket: {bucket_name}[/yellow]")
                    continue
                
                buckets.append(bucket)
                self.console.print(f"Found bucket: {bucket_name}")

            if not buckets:
                self.console.print("[yellow]No buckets to delete[/yellow]")
                return

            # Delete each bucket
            self.console.print(f"\n[bold cyan]Found {len(buckets)} buckets to delete[/bold cyan]")
            for bucket in buckets:
                bucket_name = bucket.get('Name')
                try:
                    self.console.print(f"\n[bold]Processing bucket: {bucket_name}[/bold]")
                    
                    # First, delete all objects and versions
                    self.console.print("\n[cyan]Deleting bucket contents...[/cyan]")
                    
                    # Delete all object versions
                    paginator = self.s3.get_paginator('list_object_versions')
                    for page in paginator.paginate(Bucket=bucket_name):
                        versions = []
                        if 'Versions' in page:
                            versions.extend([{'Key': v['Key'], 'VersionId': v['VersionId']} 
                                          for v in page['Versions']])
                        if 'DeleteMarkers' in page:
                            versions.extend([{'Key': v['Key'], 'VersionId': v['VersionId']} 
                                          for v in page['DeleteMarkers']])
                        
                        if versions:
                            self.s3.delete_objects(
                                Bucket=bucket_name,
                                Delete={'Objects': versions}
                            )
                            self.console.print(f"[green]✓ Deleted {len(versions)} objects/versions[/green]")

                    # Then delete the bucket
                    self.console.print("\n[cyan]Deleting bucket...[/cyan]")
                    self.s3.delete_bucket(Bucket=bucket_name)
                    self.console.print(f"[green]✓ Successfully deleted bucket: {bucket_name}[/green]")
                    
                except ClientError as e:
                    self.console.print(f"[red]✗ Error deleting bucket {bucket_name}: {str(e)}[/red]")
                    continue

            self.console.print("\n[bold green]S3 bucket cleanup completed![/bold green]")

        except ClientError as e:
            self.console.print(f"[red]Error cleaning up S3 buckets: {str(e)}[/red]")
            raise
        except Exception as e:
            self.console.print(f"[red]Error cleaning up S3 buckets: {str(e)}[/red]")
            raise

    def cleanup_iam(self):
        """Clean up IAM roles and policies"""
        try:
            # List all roles
            self.console.print("[bold cyan]Listing IAM roles...[/bold cyan]")
            roles = []
            paginator = self.iam.get_paginator('list_roles')
            
            for page in paginator.paginate():
                for role in page.get('Roles', []):
                    role_name = role.get('RoleName', '')
                    if role_name.lower().startswith('esp'):
                        roles.append(role)
                        self.console.print(f"Found role: {role_name}")

            if not roles:
                self.console.print("[yellow]No matching roles found[/yellow]")
                return

            # Delete each role
            self.console.print(f"\n[bold cyan]Found {len(roles)} roles to delete[/bold cyan]")
            for role in roles:
                role_name = role.get('RoleName')
                try:
                    self.console.print(f"\n[bold]Processing role: {role_name}[/bold]")
                    
                    # First, detach all managed policies
                    self.console.print("\n[cyan]Detaching managed policies...[/cyan]")
                    attached_policies = self.iam.list_attached_role_policies(RoleName=role_name)
                    for policy in attached_policies.get('AttachedPolicies', []):
                        try:
                            self.iam.detach_role_policy(
                                RoleName=role_name,
                                PolicyArn=policy['PolicyArn']
                            )
                            self.console.print(f"[green]✓ Detached policy: {policy['PolicyName']}[/green]")
                        except ClientError as e:
                            self.console.print(f"[yellow]⚠ Warning: Could not detach policy {policy['PolicyName']}: {str(e)}[/yellow]")

                    # Then, delete all inline policies
                    self.console.print("\n[cyan]Deleting inline policies...[/cyan]")
                    inline_policies = self.iam.list_role_policies(RoleName=role_name)
                    for policy_name in inline_policies.get('PolicyNames', []):
                        try:
                            self.iam.delete_role_policy(
                                RoleName=role_name,
                                PolicyName=policy_name
                            )
                            self.console.print(f"[green]✓ Deleted inline policy: {policy_name}[/green]")
                        except ClientError as e:
                            self.console.print(f"[yellow]⚠ Warning: Could not delete inline policy {policy_name}: {str(e)}[/yellow]")

                    # Finally, delete the role
                    self.console.print("\n[cyan]Deleting role...[/cyan]")
                    self.iam.delete_role(RoleName=role_name)
                    self.console.print(f"[green]✓ Successfully deleted role: {role_name}[/green]")
                    
                except ClientError as e:
                    self.console.print(f"[red]✗ Error deleting role {role_name}: {str(e)}[/red]")
                    continue

            self.console.print("\n[bold green]Cleanup completed![/bold green]")

        except ClientError as e:
            self.console.print(f"[red]Error cleaning up IAM roles: {str(e)}[/red]")
            raise
        except Exception as e:
            self.console.print(f"[red]Error cleaning up IAM roles: {str(e)}[/red]")
            raise

    def cleanup_cloudfront(self):
        """Clean up CloudFront distributions and custom policies"""
        try:
            # List all distributions
            self.console.print("[bold cyan]Listing CloudFront distributions...[/bold cyan]")
            distributions = []
            paginator = self.cloudfront.get_paginator('list_distributions')
            
            for page in paginator.paginate():
                for distribution in page.get('DistributionList', {}).get('Items', []):
                    distribution_id = distribution.get('Id')
                    domain_name = distribution.get('DomainName')
                    if domain_name and domain_name.lower().startswith('esp'):
                        distributions.append(distribution)
                        self.console.print(f"Found distribution: {domain_name} (ID: {distribution_id})")

            # Delete each distribution
            if distributions:
                self.console.print(f"\n[bold cyan]Found {len(distributions)} distributions to delete[/bold cyan]")
                for distribution in distributions:
                    distribution_id = distribution.get('Id')
                    domain_name = distribution.get('DomainName')
                    try:
                        self.console.print(f"\n[bold]Processing distribution: {domain_name}[/bold]")
                        
                        # Get the ETag for the distribution
                        response = self.cloudfront.get_distribution_config(Id=distribution_id)
                        etag = response.get('ETag')
                        
                        # Disable the distribution first
                        config = response.get('DistributionConfig')
                        config['Enabled'] = False
                        
                        self.console.print("\n[cyan]Disabling distribution...[/cyan]")
                        self.cloudfront.update_distribution(
                            Id=distribution_id,
                            DistributionConfig=config,
                            IfMatch=etag
                        )
                        
                        # Wait for the distribution to be disabled
                        self.console.print("[cyan]Waiting for distribution to be disabled...[/cyan]")
                        waiter = self.cloudfront.get_waiter('distribution_deployed')
                        waiter.wait(
                            Id=distribution_id,
                            WaiterConfig={'Delay': 30, 'MaxAttempts': 60}  # 30 minutes timeout
                        )
                        
                        # Delete the distribution
                        self.console.print("\n[cyan]Deleting distribution...[/cyan]")
                        self.cloudfront.delete_distribution(
                            Id=distribution_id,
                            IfMatch=etag
                        )
                        
                        self.console.print(f"[green]✓ Successfully deleted distribution: {domain_name}[/green]")
                        
                    except ClientError as e:
                        error_code = e.response['Error']['Code']
                        error_message = e.response['Error']['Message']
                        self.console.print(f"[red]✗ Error deleting distribution {domain_name}: {error_code} - {error_message}[/red]")
                        continue

            # List and delete custom policies
            self.console.print("\n[bold cyan]Listing CloudFront custom policies...[/bold cyan]")
            try:
                response = self.cloudfront.list_field_level_encryption_configs()
                policies = response.get('FieldLevelEncryptionList', {}).get('Items', [])
                
                for policy in policies:
                    policy_id = policy.get('Id')
                    policy_name = policy.get('Name')
                    if policy_name and policy_name.lower().startswith('esp'):
                        self.console.print(f"Found custom policy: {policy_name} (ID: {policy_id})")
                        
                        try:
                            self.console.print(f"\n[bold]Processing custom policy: {policy_name}[/bold]")
                            
                            # Delete the custom policy
                            self.console.print("\n[cyan]Deleting custom policy...[/cyan]")
                            self.cloudfront.delete_field_level_encryption_config(Id=policy_id)
                            
                            self.console.print(f"[green]✓ Successfully deleted custom policy: {policy_name}[/green]")
                            
                        except ClientError as e:
                            error_code = e.response['Error']['Code']
                            error_message = e.response['Error']['Message']
                            self.console.print(f"[red]✗ Error deleting custom policy {policy_name}: {error_code} - {error_message}[/red]")
                            continue
                            
            except ClientError as e:
                error_code = e.response['Error']['Code']
                error_message = e.response['Error']['Message']
                self.console.print(f"[yellow]⚠ Warning: Could not list custom policies: {error_code} - {error_message}[/yellow]")

            self.console.print("\n[bold green]CloudFront cleanup completed![/bold green]")

        except ClientError as e:
            error_code = e.response['Error']['Code']
            error_message = e.response['Error']['Message']
            self.console.print(f"[red]Error cleaning up CloudFront resources: {error_code} - {error_message}[/red]")
            raise
        except Exception as e:
            self.console.print(f"[red]Error cleaning up CloudFront resources: {str(e)}[/red]")
            raise

    def cleanup_sns(self):
        """Clean up SNS topics, subscriptions, and push notification platforms"""
        try:
            # List all topics
            self.console.print("[bold cyan]Listing SNS topics...[/bold cyan]")
            topics = []
            paginator = self.sns.get_paginator('list_topics')
            
            for page in paginator.paginate():
                for topic in page.get('Topics', []):
                    topics.append(topic)
                    self.console.print(f"Found topic: {topic.get('TopicArn')}")

            if not topics:
                self.console.print("[yellow]No topics found[/yellow]")
            else:
                # Delete each topic
                self.console.print(f"\n[bold cyan]Found {len(topics)} topics to delete[/bold cyan]")
                for topic in topics:
                    topic_arn = topic.get('TopicArn')
                    try:
                        self.console.print(f"\n[bold]Processing topic: {topic_arn}[/bold]")
                        
                        # Delete all subscriptions for the topic
                        self.console.print("\n[cyan]Deleting subscriptions...[/cyan]")
                        paginator = self.sns.get_paginator('list_subscriptions_by_topic')
                        for page in paginator.paginate(TopicArn=topic_arn):
                            for sub in page.get('Subscriptions', []):
                                try:
                                    self.sns.unsubscribe(SubscriptionArn=sub['SubscriptionArn'])
                                    self.console.print(f"[green]✓ Unsubscribed: {sub['SubscriptionArn']}[/green]")
                                except ClientError as e:
                                    self.console.print(f"[yellow]⚠ Warning: Could not unsubscribe {sub['SubscriptionArn']}: {str(e)}[/yellow]")

                        # Delete the topic
                        self.console.print("\n[cyan]Deleting topic...[/cyan]")
                        self.sns.delete_topic(TopicArn=topic_arn)
                        self.console.print(f"[green]✓ Successfully deleted topic: {topic_arn}[/green]")
                        
                    except ClientError as e:
                        error_code = e.response['Error']['Code']
                        error_message = e.response['Error']['Message']
                        self.console.print(f"[red]✗ Error deleting topic {topic_arn}: {error_code} - {error_message}[/red]")
                        continue

            # List and delete push notification platforms
            self.console.print("\n[bold cyan]Listing SNS push notification platforms...[/bold cyan]")
            
            # List SMS platforms
            try:
                sms_platforms = []
                paginator = self.sns.get_paginator('list_platform_applications')
                for page in paginator.paginate():
                    for platform in page.get('PlatformApplications', []):
                        sms_platforms.append(platform)
                        self.console.print(f"Found SMS platform: {platform.get('PlatformApplicationArn')}")

                # Delete SMS platforms
                if sms_platforms:
                    self.console.print(f"\n[bold cyan]Found {len(sms_platforms)} SMS platforms to delete[/bold cyan]")
                    for platform in sms_platforms:
                        platform_arn = platform.get('PlatformApplicationArn')
                        try:
                            self.console.print(f"\n[bold]Processing SMS platform: {platform_arn}[/bold]")
                            
                            # Delete all endpoints for this platform
                            self.console.print("\n[cyan]Deleting platform endpoints...[/cyan]")
                            paginator = self.sns.get_paginator('list_endpoints_by_platform_application')
                            for page in paginator.paginate(PlatformApplicationArn=platform_arn):
                                for endpoint in page.get('Endpoints', []):
                                    try:
                                        self.sns.delete_endpoint(EndpointArn=endpoint['EndpointArn'])
                                        self.console.print(f"[green]✓ Deleted endpoint: {endpoint['EndpointArn']}[/green]")
                                    except ClientError as e:
                                        self.console.print(f"[yellow]⚠ Warning: Could not delete endpoint {endpoint['EndpointArn']}: {str(e)}[/yellow]")

                            # Delete the platform
                            self.console.print("\n[cyan]Deleting SMS platform...[/cyan]")
                            self.sns.delete_platform_application(PlatformApplicationArn=platform_arn)
                            self.console.print(f"[green]✓ Successfully deleted SMS platform: {platform_arn}[/green]")
                            
                        except ClientError as e:
                            error_code = e.response['Error']['Code']
                            error_message = e.response['Error']['Message']
                            self.console.print(f"[red]✗ Error deleting SMS platform {platform_arn}: {error_code} - {error_message}[/red]")
                            continue
                else:
                    self.console.print("[yellow]No SMS platforms found[/yellow]")
                    
            except ClientError as e:
                error_code = e.response['Error']['Code']
                error_message = e.response['Error']['Message']
                self.console.print(f"[yellow]⚠ Warning: Could not list SMS platforms: {error_code} - {error_message}[/yellow]")

            # List and delete email endpoints
            try:
                email_endpoints = []
                paginator = self.sns.get_paginator('list_endpoints_by_platform_application')
                # Note: Email endpoints are typically created without a platform application
                # We'll try to list all endpoints and filter for email ones
                try:
                    # This might not work for all cases, but we'll try
                    response = self.sns.list_endpoints_by_platform_application(PlatformApplicationArn='dummy')
                except:
                    # If we can't list all endpoints, we'll skip email endpoint cleanup
                    self.console.print("[yellow]⚠ Warning: Could not list email endpoints (this is normal)[/yellow]")
                    email_endpoints = []

                if email_endpoints:
                    self.console.print(f"\n[bold cyan]Found {len(email_endpoints)} email endpoints to delete[/bold cyan]")
                    for endpoint in email_endpoints:
                        endpoint_arn = endpoint.get('EndpointArn')
                        try:
                            self.console.print(f"\n[bold]Processing email endpoint: {endpoint_arn}[/bold]")
                            
                            # Delete the endpoint
                            self.console.print("\n[cyan]Deleting email endpoint...[/cyan]")
                            self.sns.delete_endpoint(EndpointArn=endpoint_arn)
                            self.console.print(f"[green]✓ Successfully deleted email endpoint: {endpoint_arn}[/green]")
                            
                        except ClientError as e:
                            error_code = e.response['Error']['Code']
                            error_message = e.response['Error']['Message']
                            self.console.print(f"[red]✗ Error deleting email endpoint {endpoint_arn}: {error_code} - {error_message}[/red]")
                            continue
                            
            except ClientError as e:
                error_code = e.response['Error']['Code']
                error_message = e.response['Error']['Message']
                self.console.print(f"[yellow]⚠ Warning: Could not list email endpoints: {error_code} - {error_message}[/yellow]")

            self.console.print("\n[bold green]SNS cleanup completed![/bold green]")

        except ClientError as e:
            error_code = e.response['Error']['Code']
            error_message = e.response['Error']['Message']
            self.console.print(f"[red]Error cleaning up SNS resources: {error_code} - {error_message}[/red]")
            raise
        except Exception as e:
            self.console.print(f"[red]Error cleaning up SNS resources: {str(e)}[/red]")
            raise

    def cleanup_aws_resources(self):
        """Clean up AWS resources based on specified services"""
        try:
            if self.services == ['all'] or 'cloudformation' in self.services:
                self.cleanup_cloudformation_stacks()

            if self.services == ['all'] or 's3' in self.services:
                self.cleanup_s3_buckets()
            
            if self.services == ['all'] or 'iam' in self.services:
                self.cleanup_iam()
            
            if self.services == ['all'] or 'lambda' in self.services:
                self.cleanup_lambda_functions()
            
            if self.services == ['all'] or 'cognito' in self.services:
                self.cleanup_cognito()
            
            if self.services == ['all'] or 'dynamodb' in self.services:
                self.cleanup_dynamodb()
            
            if self.services == ['all'] or 'cloudwatch' in self.services:
                self.cleanup_cloudwatch()
            
            if self.services == ['all'] or 'cloudfront' in self.services:
                self.cleanup_cloudfront()
            
            if self.services == ['all'] or 'iot' in self.services:
                self.cleanup_iot()
            
            if self.services == ['all'] or 'sns' in self.services:
                self.cleanup_sns()
            
            self.console.print("\n[bold green]AWS cleanup completed![/bold green]")
            
        except Exception as e:
            self.console.print(f"[red]Error during cleanup: {str(e)}[/red]")
            raise

@click.command()
@click.option('--profile', help='AWS profile name to use')
@click.option('--access-key', help='AWS access key ID')
@click.option('--secret-key', help='AWS secret access key')
@click.option('--session-token', help='AWS session token')
@click.option('--region', default='us-east-1', help='AWS region name (e.g., us-east-1, eu-west-1)')
@click.option('--service', type=click.Choice(['iam', 's3', 'lambda', 'cloudformation', 'cognito', 'dynamodb', 'iot', 'cloudwatch', 'cloudfront', 'sns', 'all']), default='all',
              help='Service to clean up (iam=IAM roles, s3=S3 buckets, lambda=Lambda functions, cloudformation=CloudFormation stacks, cognito=Cognito user pools, dynamodb=DynamoDB tables, iot=IoT resources, cloudwatch=CloudWatch resources, cloudfront=CloudFront resources, sns=SNS topics and subscriptions, all=all services)')
def main(profile, access_key, secret_key, session_token, region, service):
    """AWS Deployment Cleanup Tool"""
    cleanup = AWSDeployCleanup(profile, access_key, secret_key, session_token, region, [service])
    
    with Progress() as progress:
        task = progress.add_task("[cyan]Cleaning up deployments...", total=100)
        
        try:
            cleanup.cleanup_aws_resources()
            progress.update(task, completed=100)
        except Exception as e:
            progress.update(task, completed=0)
            raise click.ClickException(str(e))

if __name__ == '__main__':
    main() 