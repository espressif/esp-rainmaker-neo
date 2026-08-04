#!/usr/bin/env python3
"""Delete the Lambda log groups that CloudFormation is about to take ownership of.

`create_lambda_function` used to cap retention through the deprecated `log_retention`
prop, which set retention on a log group CloudFormation never owned — Lambda creates
`/aws/lambda/<name>` itself on first invocation. Those functions now declare a real
`AWS::Logs::LogGroup`, and CloudFormation cannot create over a group that already
exists: the stack update fails the CREATE with "already exists" and rolls back.

Run this immediately before the first deploy that carries that change.

`cdk deploy --import-existing-resources` is NOT an alternative: CloudFormation only
auto-imports resources whose DeletionPolicy is Retain or RetainExceptOnCreate, and these
groups are RemovalPolicy.DESTROY on purpose (retaining would strand them on `cdk destroy`
and collide again on the next deploy). Deleting is the supported path; with 7-day
retention it costs at most a week of logs.

Targets come from synthesized templates, so the list stays correct as lambdas are added.
Only groups CDK newly manages are considered (logical id prefix `LambdaLogGrp`); the
`iot-rules/*` groups have always been CloudFormation-owned and are left untouched.

SCOPE: only what is in the directories you pass. cdk/apps/ holds five independent apps
(rmng, espuser, claim, alexa, test_infra) and one app's cdk.out never contains another's
stacks — synth every app you deploy, or you will clear one app and still fail on the next.

REGIONS: log groups are regional. The Alexa skill Lambda is special: it deploys to the
three Alexa-supported regions (us-east-1, eu-west-1, us-west-2) and each copy is named
after the *rmng* region it serves, so a single rmng deployment in eu-west-2 owns
/aws/lambda/rmng-alexa-skill-eu-west-2 in all three of those regions. Pass --region
more than once (or a comma list) to sweep them in one run.

NAME RESOLUTION AND WHY IT MATTERS: LogGroupName is usually a plain string. The Alexa
skill's is an Fn::Join embedding `Ref: RmngRegion`. We resolve Refs against the
template's own Parameters defaults (and AWS::Region against the target region), which
yields an exact name. That precision is a safety property, not a nicety: the literal
head of that Join is "/aws/lambda/rmng-alexa-skill-", and deleting by that prefix in a
shared account also destroys the log groups of every *other* rmng deployment
(…-ap-south-1, …-eu-west-1, …). Unresolvable names therefore fall back to a prefix that
is reported but NOT deleted unless you pass --allow-prefix-delete.

Usage:
    for a in rmng espuser claim alexa test_infra; do
        cdk synth --quiet --app "python3 cdk/apps/$a.py" --output cdk/cdk.out.$a
    done
    python scripts/delete_unmanaged_lambda_log_groups.py cdk/cdk.out.*                     # dry run, all apps
    python scripts/delete_unmanaged_lambda_log_groups.py cdk/cdk.out.* --apply

    # Alexa: same rmng region, three Alexa regions, one run
    python scripts/delete_unmanaged_lambda_log_groups.py cdk/cdk.out.alexa --apply \
        --region us-east-1,eu-west-1,us-west-2
"""

import argparse
import json
import os
import sys
from pathlib import Path

import boto3
from botocore.exceptions import ClientError

MANAGED_LOGICAL_ID_PREFIX = "LambdaLogGrp"


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


def resolve_ref(part, params, region):
    """Resolve a {"Ref": ...} to a literal, or None if we cannot know its value.

    Template parameter defaults are authoritative here: the deploy passes the same
    values, and a parameter with no default would make the group name unknowable
    anyway. AWS::Region resolves to the region we are pointed at.
    """
    if not (isinstance(part, dict) and set(part) == {"Ref"}):
        return None
    ref = part["Ref"]
    if ref == "AWS::Region":
        return region
    default = params.get(ref, {}).get("Default")
    return default if isinstance(default, str) and default else None


def resolve_name(name, params, region):
    """Return (exact_name, prefix). Exactly one is non-None; prefix means unresolved.

    A fully resolved name is deletable safely. A prefix is reported for diagnosis but
    must never be deleted implicitly — see the module docstring.
    """
    if isinstance(name, str):
        return name, None
    if isinstance(name, dict) and "Fn::Join" in name:
        sep, parts = name["Fn::Join"]
        resolved, head, still_literal = [], [], True
        for part in parts:
            lit = part if isinstance(part, str) else resolve_ref(part, params, region)
            if lit is None:
                resolved = None
                break
            resolved.append(lit)
            if still_literal and isinstance(part, str):
                head.append(part)
            else:
                still_literal = False
        if resolved is not None:
            return sep.join(resolved), None
        if not head:
            return None, None
        prefix = sep.join(head)
        return None, (prefix + sep if len(head) < len(parts) else prefix)
    return None, None


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
    """Return (exact_names, prefixes) for every LambdaLogGrp resource, resolved for region."""
    exact, prefixes = set(), set()
    for tpl, doc in templates:
        params = doc.get("Parameters", {})
        for logical_id, res in doc.get("Resources", {}).items():
            if res.get("Type") != "AWS::Logs::LogGroup":
                continue
            if not logical_id.startswith(MANAGED_LOGICAL_ID_PREFIX):
                continue
            name = res.get("Properties", {}).get("LogGroupName")
            exact_name, prefix = resolve_name(name, params, region)
            if exact_name is not None:
                exact.add(exact_name)
            elif prefix is not None:
                print(f"warning: {tpl.name}/{logical_id} name is not fully resolvable, "
                      f"falling back to prefix {prefix!r}: {json.dumps(name)}", file=sys.stderr)
                prefixes.add(prefix)
            else:
                print(f"warning: {tpl.name}/{logical_id} has an unresolvable name, skipping:"
                      f" {json.dumps(name)}", file=sys.stderr)
    return exact, prefixes


def existing_groups(logs, names, prefixes, allow_prefix_delete):
    """Which targets exist right now. Returns (deletable, prefix_only_matches)."""
    found, prefix_only = set(), set()
    paginator = logs.get_paginator("describe_log_groups")

    # One describe per exact name: cheaper and clearer than listing every group in the
    # account, and it tolerates names that are prefixes of one another.
    for name in sorted(names):
        for page in paginator.paginate(logGroupNamePrefix=name):
            for grp in page["logGroups"]:
                if grp["logGroupName"] == name:
                    found.add(name)

    for prefix in sorted(prefixes):
        matched = []
        for page in paginator.paginate(logGroupNamePrefix=prefix):
            matched.extend(g["logGroupName"] for g in page["logGroups"])
        matched = [m for m in matched if m not in found]
        if not matched:
            continue
        print(f"prefix {prefix!r} matched: {', '.join(sorted(matched))}")
        if allow_prefix_delete:
            found.update(matched)
        else:
            prefix_only.update(matched)
    return found, prefix_only


def sweep_region(templates, region, profile, args):
    """Resolve, report and (optionally) delete for one region. Returns (deleted, failures)."""
    exact, prefixes = collect_targets(templates, region)
    session = make_session(profile, region)
    logs = session.client("logs")
    identity = session.client("sts").get_caller_identity()
    account = identity["Account"]
    via = "AWS_ACCESS_KEY_ID" if os.environ.get("AWS_ACCESS_KEY_ID") else (profile or "default profile")

    print(f"=== account {account}  region {region}  (credentials via {via}) ===")
    print(f"    {identity['Arn']}")
    print(f"{len(exact)} exact name(s), {len(prefixes)} unresolved prefix pattern(s)")

    found, prefix_only = existing_groups(logs, exact, prefixes, args.allow_prefix_delete)
    absent = sorted(exact - found)

    if absent:
        print(f"{len(absent)} target(s) absent — CloudFormation will create them cleanly:")
        for name in absent:
            print(f"  - {name}")

    if prefix_only:
        print(f"\n{len(prefix_only)} group(s) matched only an UNRESOLVED PREFIX and were NOT "
              f"deleted. These may belong to other deployments; deleting them would destroy "
              f"their logs. Pass --allow-prefix-delete only if you are certain:")
        for name in sorted(prefix_only):
            print(f"  ? {name}")

    if not found:
        print("Nothing to delete in this region.\n")
        return 0, 0

    print(f"\n{len(found)} log group(s) WILL BE DELETED, along with all their log history:")
    for name in sorted(found):
        print(f"  x {name}")

    if not args.apply:
        print("Dry run. Re-run with --apply to delete.\n")
        return 0, 0

    if not args.yes:
        print("Lambda recreates these on the next invocation, so deploy immediately after "
              "this completes — ideally with traffic quiesced.")
        if input(f"Delete {len(found)} log group(s) in {region}? [y/N] ").strip().lower() != "y":
            print("Aborted for this region.\n")
            return 0, 0

    deleted = failures = 0
    for name in sorted(found):
        try:
            logs.delete_log_group(logGroupName=name)
            print(f"deleted {name}")
            deleted += 1
        except ClientError as e:
            code = e.response["Error"]["Code"]
            if code == "ResourceNotFoundException":
                print(f"already gone {name}")
            else:
                print(f"FAILED {name}: {code}", file=sys.stderr)
                failures += 1
    print()
    return deleted, failures


def main():
    ap = argparse.ArgumentParser(
        description="Delete unmanaged /aws/lambda log groups before CDK takes them over.",
        epilog="Pass every app's cdk.out (rmng, espuser, claim, alexa, test_infra). "
               "Repeat --region (or use a comma list) for multi-region stacks like alexa.",
    )
    ap.add_argument("paths", nargs="+", help="cdk.out directories or *.template.json files")
    ap.add_argument("--apply", action="store_true", help="actually delete (default is a dry run)")
    ap.add_argument("--yes", action="store_true", help="skip the confirmation prompt")
    ap.add_argument("--region", action="append", default=None,
                    help="AWS region; repeatable, or a comma-separated list "
                         "(default: $AWS_REGION, then $AWS_DEFAULT_REGION, then the "
                         "--profile's configured region)")
    ap.add_argument("--profile", default=os.environ.get("AWS_PROFILE"), help="AWS profile")
    ap.add_argument("--allow-prefix-delete", action="store_true",
                    help="also delete groups matched only by an unresolved name prefix. "
                         "DANGEROUS: a prefix can match other deployments' log groups.")
    args = ap.parse_args()

    raw = args.region or ([default_region(args.profile)] if default_region(args.profile) else [])
    regions = [r.strip() for spec in raw for r in spec.split(",") if r.strip()]
    if not regions:
        print("error: no region; pass --region, or set AWS_REGION / AWS_DEFAULT_REGION, "
              "or configure a region for --profile", file=sys.stderr)
        return 2

    templates = load_templates(args.paths)
    if not templates:
        print("No templates found. Did you synth first?")
        return 1
    print(f"scanned {len(templates)} template(s) across {len(regions)} region(s)\n")

    total_deleted = total_failures = 0
    for region in regions:
        d, f = sweep_region(templates, region, args.profile, args)
        total_deleted += d
        total_failures += f

    if not args.apply:
        return 0
    print(f"Done. {total_deleted} deleted, {total_failures} failed.")
    if total_failures:
        return 3
    print("Deploy now, before traffic recreates these groups.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
