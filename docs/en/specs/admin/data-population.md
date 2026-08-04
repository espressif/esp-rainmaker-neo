# Populating a deployment with data

This page is a **contributor walkthrough**, not a specification: it shows how a
fresh deployment is seeded with enough data for the admin dashboard to have
something to display, using the repository's own simulators and integration-test
harness. For the contracts behind each step, follow the links to the specs.

## Step 1: Create super admin user

Configured in `cli/test_config.json`:
```json
{
  "users": [
    {
      "name": "super_admin@example.com",
      "password": "<strong-password>",
      "super_admin": true
    }
  ]
}
```

The integration-test harness creates this user for you (`test/itest/conftest.py`), registering it in the admin Cognito pool and setting `custom:super_admin = "true"`. See [Authentication and Permissions](authentication.md) for what that claim buys.

## Step 2: Register nodes

```python
# Single node
super_admin_user.register_node(device,
    admin_group_names=["my-group"],
    tags=["env:prod", "created_by:admin"])

# Bulk (CSV in S3: node_id,cert,admin_groups)
resp = super_admin_user.bulk_register_nodes(s3_path,
    admin_group_names=["batch-1"],
    tags=["env:staging"])
request_id = resp.get("request_id")

# Poll status
status = super_admin_user.get_bulk_register_status(request_id)
```

## Step 3: Associate nodes with user groups

```python
# Regular user creates group
group_id = group_api.create_group("My Group")

# Device connects MQTT, user confirms association
user.do_user_node_assoc(device, group_id)
device.wait_for_group_info()
```

## Step 4: Device reports shadow data

Once connected, the device publishes to `$aws/things/{thingName}/shadow/name/iparams/update` with reported state. This populates the `iparams` shadow, which fleet indexing picks up and makes available to `SearchIndex` — see [Indexed Parameters](../device_management.md) for the document shape and [Fleet Indexing](fleet-indexing.md) for what is indexed.

## Step 5: Dashboard reads data

1. Admin signs in via Admin Cognito Pool
2. Gets AWS credentials via Identity Pool (`AdminDeviceUsersRole`)
3. Calls AWS IoT SDK directly: `SearchIndex`, `ListThings`, `DescribeThingGroup`, `ListJobs`, etc.
4. Calls ESP RainMaker Neo APIs via API Gateway for admin-specific operations

## Where each piece lives

| File                                   | Purpose                                                                                      |
| -------------------------------------- | -------------------------------------------------------------------------------------------- |
| `test/app_sim.py`                      | App simulator                                                                                |
| `test/device_sim.py`                   | Device simulator (MQTT connect, shadow publish)                                              |
| `test/itest/conftest.py`               | Admin user fixture (`_init_admin_user()`, `super_admin_user`)                                |
| `test/itest/test_admin.py`             | Admin auth, role assumption, group/subgroup access                                           |
| `test/itest/test_node_registration.py` | Single & bulk registration with admin groups and tags                                        |
| `test/test_user.py`                    | `register_node()`, `bulk_register_nodes()`, `assume_role_admin()`, `admin_get_node_groups()` |
