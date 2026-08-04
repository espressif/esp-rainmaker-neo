#!/usr/bin/env python3
"""Remove logs:CreateLogGroup from the execution roles of our own Lambdas.

Log groups are now declared as `AWS::Logs::LogGroup` in the stacks, so a function's role no
longer needs to create its own. Two things grant it today:

  * the AWS-managed policy `service-role/AWSLambdaBasicExecutionRole`, which is
    `logs:CreateLogGroup + CreateLogStream + PutLogEvents` on `*` and cannot be edited — it is
    detached, and the two actions still needed are re-granted inline, scoped to that
    function's own /aws/lambda/<name> group;
  * inline policy statements listing CreateLogGroup, from which that one action is dropped.

SCOPE — WHY TARGETS COME FROM TEMPLATES: a Lambda-trust-policy sweep of an account matches
~140 roles here, of which only a handful are ours. The rest belong to CDK's own constructs
(custom-resource providers, S3 auto-delete, log-retention helpers) and to unrelated stacks.
Those are not ours to re-permission, and CDK overwrites them anyway. So, exactly as
delete_unmanaged_lambda_log_groups.py does, targets are read from synthesized templates: a
function is ours when the template declares it with an explicit FunctionName AND a CDK-managed
log group. CDK-internal lambdas have neither — they take generated names and let the service
create the group — so they are excluded by construction rather than by a name blocklist.

WHAT THIS DOES NOT DO: it does not stop `/aws/lambda/<name>` from appearing. When a function
is invoked and its group is missing, the Lambda *service* creates it under its own privileges,
not the execution role's. Removing the permission only stops function code from calling
CreateLogGroup itself. Declaring the group in CloudFormation is what prevents implicit
creation; this is least-privilege cleanup, not a fix for name conflicts.

DRIFT: these roles are owned by CloudFormation. Editing them here diverges from the templates,
and the next `cdk deploy` restores whatever the stack says. Run this to correct already-deployed
roles ahead of a deploy carrying the same change in source (cdk/utils/app_common.py), or accept
that the change is temporary.

REGIONS: IAM roles are global, but --region also resolves template Refs (e.g. RmngRegion,
AWS::Region) to the same values the deploy uses, and scopes the replacement grant's ARN. The
Alexa skill lambda's role and log group name both embed RmngRegion, and that lambda deploys to
three fixed regions regardless of which region rmng itself runs in — sweep those three
explicitly, same as delete_unmanaged_lambda_log_groups.py.

Usage:
    for a in rmng espuser claim alexa test_infra; do
        cdk synth --quiet --app "python3 cdk/apps/$a.py" --output cdk/cdk.out.$a
    done
    python scripts/strip_lambda_creategroup_permission.py cdk/cdk.out.*            # dry run
    python scripts/strip_lambda_creategroup_permission.py cdk/cdk.out.* --apply

    # Alexa: same rmng region, three Alexa regions, one run each
    for r in us-east-1 eu-west-1 us-west-2; do
        python scripts/strip_lambda_creategroup_permission.py cdk/cdk.out.alexa --region "$r" --apply
    done
"""

import argparse
import json
import os
import sys
from pathlib import Path

import boto3
from botocore.exceptions import ClientError

BASIC_EXECUTION_ARN = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"


def default_region(profile):
    """Region AWS itself would pick: $AWS_REGION, then $AWS_DEFAULT_REGION, then the
    given profile's configured region (~/.aws/config), in that order — same precedence
    the AWS CLI and SDKs use. --region overrides all of this; this is only the fallback."""
    return (os.environ.get("AWS_REGION") or os.environ.get("AWS_DEFAULT_REGION")
            or boto3.Session(profile_name=profile).region_name)


def make_session(profile, region):
    """A session honoring keys > profile > default, deliberately NOT botocore's own order.

    Passing profile_name=<anything> to boto3.Session removes the `env` credential provider
    from the chain entirely, so a named profile always wins over AWS_ACCESS_KEY_ID/
    AWS_SECRET_ACCESS_KEY/AWS_SESSION_TOKEN even if they are set — profile beats keys, not
    the other way round. To get keys > profile > default instead, profile_name is passed
    only when no literal keys are present; boto3 then falls through to its own default
    profile if --profile/$AWS_PROFILE was empty too.

    This is the requested ordering, not the safer one: a stray AWS_ACCESS_KEY_ID left in the
    shell silently overrides an explicit --profile, with nothing printed to flag it. Callers
    should surface the resolved account/ARN before doing anything destructive.
    """
    if os.environ.get("AWS_ACCESS_KEY_ID"):
        return boto3.Session(region_name=region)
    return boto3.Session(region_name=region, profile_name=profile)


TARGET_ACTION = "logs:createloggroup"
KEEP_ACTIONS = ["logs:CreateLogStream", "logs:PutLogEvents"]


def as_list(value):
    """IAM renders a single-element Action/Resource/Statement as a bare value, not a list."""
    if value is None:
        return []
    return value if isinstance(value, list) else [value]


def load_templates(paths):
    """Every *.template.json under the given dirs/files, as parsed JSON."""
    templates = []
    for p in paths:
        path = Path(p)
        if path.is_dir():
            files = sorted(path.glob("*.template.json"))
        elif path.is_file():
            files = [path]
        else:
            print(f"warning: {p} does not exist, skipping", file=sys.stderr)
            continue
        for f in files:
            try:
                templates.append((f, json.loads(f.read_text())))
            except (OSError, json.JSONDecodeError) as e:
                print(f"warning: cannot read {f}: {e}", file=sys.stderr)
    return templates


def collect_targets(templates, region):
    """Map physical role name -> function name, for our Lambdas only.

    A function qualifies when the template gives it a literal FunctionName and a log group we
    manage. Both conditions exclude CDK-internal lambdas, whose names CloudFormation generates
    and whose groups the Lambda service creates.
    """
    targets, unresolved = {}, []
    for tpl, doc in templates:
        resources = doc.get("Resources", {})
        params = doc.get("Parameters", {})
        # A logical id counts as CDK-managed regardless of whether its LogGroupName resolves
        # here — the Alexa skill's is an Fn::Join over {Ref: RmngRegion} — because owning the
        # resource, not knowing its literal name, is what LoggingConfig.LogGroup actually needs.
        managed_groups = {
            lid for lid, res in resources.items() if res.get("Type") == "AWS::Logs::LogGroup"
        }
        for logical_id, res in resources.items():
            if res.get("Type") != "AWS::Lambda::Function":
                continue
            props = res.get("Properties", {})
            fn_name = resolve_name(props.get("FunctionName"), params, region)
            if fn_name is None:
                continue  # generated name, or unresolvable => not ours to guess at
            log_cfg = props.get("LoggingConfig") or {}
            group_ref = log_cfg.get("LogGroup")
            if not (isinstance(group_ref, dict) and group_ref.get("Ref") in managed_groups):
                continue  # no CDK-managed group => service-created, not ours

            role_name = resolve_role_name(props.get("Role"), resources, params, region)
            if role_name is None:
                unresolved.append((tpl.name, logical_id, fn_name))
                continue
            targets[role_name] = fn_name
    return targets, unresolved


def resolve_name(value, params, region):
    """Resolve a template string that may be an Fn::Join over Refs, or None if unknowable.

    Role names here are `Fn::Join ["", ["<lambda>-role-", {"Ref": "AWS::Region"}]]`. Refs are
    resolved against AWS::Region and the template's own parameter defaults, which the deploy
    passes too. Anything left unresolved yields None rather than a guess — a wrong role name
    would mean editing an unrelated role's permissions.
    """
    if isinstance(value, str):
        return value
    if not isinstance(value, dict):
        return None
    if set(value) == {"Ref"}:
        ref = value["Ref"]
        if ref == "AWS::Region":
            return region
        default = params.get(ref, {}).get("Default")
        return default if isinstance(default, str) and default else None
    if "Fn::Join" in value:
        sep, parts = value["Fn::Join"]
        resolved = [resolve_name(p, params, region) for p in parts]
        return sep.join(resolved) if all(r is not None for r in resolved) else None
    return None


def resolve_role_name(role_ref, resources, params, region):
    """Physical RoleName behind a function's `Role: {Fn::GetAtt: [<id>, Arn]}`."""
    if not (isinstance(role_ref, dict) and "Fn::GetAtt" in role_ref):
        return None
    logical_id = role_ref["Fn::GetAtt"][0]
    role = resources.get(logical_id, {})
    if role.get("Type") != "AWS::IAM::Role":
        return None
    return resolve_name(role.get("Properties", {}).get("RoleName"), params, region)


def strip_action(doc):
    """Drop logs:CreateLogGroup from a policy document.

    Returns (new_doc, removed). A statement whose only action was CreateLogGroup is dropped
    whole — an empty Action list is not a valid policy. `removed` counts affected statements.
    """
    statements, removed = [], 0
    for stmt in as_list(doc.get("Statement")):
        actions = as_list(stmt.get("Action"))
        kept = [a for a in actions if str(a).lower() != TARGET_ACTION]
        if len(kept) == len(actions):
            statements.append(stmt)
            continue
        removed += 1
        if not kept:
            continue  # statement existed only to grant CreateLogGroup
        statements.append({**stmt, "Action": kept if len(kept) > 1 else kept[0]})
    if not removed:
        return doc, 0
    return {**doc, "Statement": statements}, removed


def plan_role(iam, role_name):
    """What would change for one role: (inline_edits, detach_managed) or None if absent."""
    try:
        policy_names = iam.list_role_policies(RoleName=role_name)["PolicyNames"]
        attached = iam.list_attached_role_policies(RoleName=role_name)["AttachedPolicies"]
    except ClientError as e:
        if e.response["Error"]["Code"] == "NoSuchEntity":
            return None
        raise

    inline_edits = []
    for policy_name in policy_names:
        doc = iam.get_role_policy(RoleName=role_name, PolicyName=policy_name)["PolicyDocument"]
        new_doc, removed = strip_action(doc)
        if removed:
            inline_edits.append((policy_name, new_doc, removed))

    detach = any(p["PolicyArn"] == BASIC_EXECUTION_ARN for p in attached)
    return inline_edits, detach


def apply_role(iam, role_name, inline_edits, detach, log_group_arn):
    """Write the planned changes for one role."""
    for policy_name, new_doc, _ in inline_edits:
        if new_doc.get("Statement"):
            iam.put_role_policy(RoleName=role_name, PolicyName=policy_name,
                                PolicyDocument=json.dumps(new_doc))
        else:
            # Every statement was a CreateLogGroup-only grant; an empty policy is invalid.
            iam.delete_role_policy(RoleName=role_name, PolicyName=policy_name)

    if detach:
        # Grant the replacement BEFORE detaching, so the function is never without logging.
        iam.put_role_policy(
            RoleName=role_name,
            PolicyName="LambdaScopedLogging",
            PolicyDocument=json.dumps({
                "Version": "2012-10-17",
                "Statement": [{
                    "Effect": "Allow",
                    "Action": KEEP_ACTIONS,
                    "Resource": log_group_arn,
                }],
            }),
        )
        iam.detach_role_policy(RoleName=role_name, PolicyArn=BASIC_EXECUTION_ARN)


def main():
    ap = argparse.ArgumentParser(
        description="Remove logs:CreateLogGroup from our Lambdas' execution roles.",
        epilog="Pass every app's cdk.out, as with delete_unmanaged_lambda_log_groups.py. "
               "Roles are read from the templates, so CDK-internal lambdas are never touched.",
    )
    ap.add_argument("paths", nargs="+", help="cdk.out directories or *.template.json files")
    ap.add_argument("--apply", action="store_true", help="actually modify roles (default is a dry run)")
    ap.add_argument("--yes", action="store_true", help="skip the confirmation prompt")
    ap.add_argument("--region", default=None,
                    help="region for the replacement grant's ARN (default: $AWS_REGION, "
                         "then $AWS_DEFAULT_REGION, then the --profile's configured region)")
    ap.add_argument("--profile", default=os.environ.get("AWS_PROFILE"), help="AWS profile")
    args = ap.parse_args()

    args.region = args.region or default_region(args.profile)
    if not args.region:
        print("error: no region; pass --region, or set AWS_REGION / AWS_DEFAULT_REGION, "
              "or configure a region for --profile", file=sys.stderr)
        return 2

    templates = load_templates(args.paths)
    if not templates:
        print("No templates found. Did you synth first?")
        return 1

    targets, unresolved = collect_targets(templates, args.region)
    for tpl, logical_id, fn_name in unresolved:
        print(f"warning: {tpl}/{logical_id} ({fn_name}) has no literal RoleName, skipping",
              file=sys.stderr)

    session = make_session(args.profile, args.region)
    iam = session.client("iam")
    ident = session.client("sts").get_caller_identity()
    account, partition = ident["Account"], ident["Arn"].split(":")[1]
    via = "AWS_ACCESS_KEY_ID" if os.environ.get("AWS_ACCESS_KEY_ID") else (args.profile or "default profile")

    print(f"scanned {len(templates)} template(s)")
    print(f"=== account {account}  region {args.region}  (credentials via {via}) ===")
    print(f"    {ident['Arn']}")
    print(f"{len(targets)} of our Lambda role(s) in the templates")

    planned, absent = [], []
    for role_name, fn_name in sorted(targets.items()):
        plan = plan_role(iam, role_name)
        if plan is None:
            absent.append(role_name)
            continue
        inline_edits, detach = plan
        if not inline_edits and not detach:
            continue
        arn = (f"arn:{partition}:logs:{args.region}:{account}"
               f":log-group:/aws/lambda/{fn_name}:*")
        planned.append((role_name, fn_name, inline_edits, detach, arn))

    if absent:
        print(f"\n{len(absent)} role(s) not deployed in this account — nothing to change:")
        for name in absent:
            print(f"  - {name}")

    for role_name, fn_name, inline_edits, detach, _ in planned:
        print(f"\n  {role_name}  ({fn_name})")
        for policy_name, new_doc, removed in inline_edits:
            fate = "delete policy" if not new_doc.get("Statement") else f"{removed} statement(s)"
            print(f"    - inline {policy_name}: drop CreateLogGroup ({fate})")
        if detach:
            print(f"    - detach AWSLambdaBasicExecutionRole, replace with "
                  f"{'/'.join(a.split(':')[1] for a in KEEP_ACTIONS)} on /aws/lambda/{fn_name}")

    if not planned:
        print("\nNothing to change.")
        return 0

    print(f"\n{len(planned)} role(s) WILL BE MODIFIED.")
    if not args.apply:
        print("Dry run. Re-run with --apply to change them.")
        return 0

    if not args.yes:
        print("These roles are CloudFormation-owned; the next deploy restores the template's "
              "version. Apply the matching source change too.")
        if input(f"Modify {len(planned)} role(s) in {account}? [y/N] ").strip().lower() != "y":
            print("Aborted.")
            return 0

    changed = failures = 0
    for role_name, _, inline_edits, detach, arn in planned:
        try:
            apply_role(iam, role_name, inline_edits, detach, arn)
            print(f"updated {role_name}")
            changed += 1
        except ClientError as e:
            print(f"FAILED {role_name}: {e.response['Error']['Code']}", file=sys.stderr)
            failures += 1

    print(f"\nDone. {changed} updated, {failures} failed.")
    return 3 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
