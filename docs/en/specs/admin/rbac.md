# RBAC & Authorization


## Permission Model

Permissions are `action:resource` pairs with wildcard support:

```
"*":"*"                    — All actions on all resources
"node:*":"*"               — All node actions on any resource
"node:get":"node-123"      — Specific action on specific resource
```

## Defined Actions

Each action family has a `<family>:*` wildcard alongside its individual actions.

**Group actions:**
```
group:*, group:create, group:createsubgroup, group:get, group:listsubentities,
group:editnodes, group:update, group:updatesubgroup, group:updatecapabilities,
group:delete, group:deletesubgroup, group:share, group:listusers,
group:listprimaryusers, group:getautomation, group:editautomation,
group:deleteautomation
```

**Node actions:**
```
node:*, node:get, node:update, node:deleteconfig, node:putconfig,
node:editgroups, node:writeshadow, node:readshadow, node:publishtodevice
```

**Node admin actions:**
```
nodeadmin:*, nodeadmin:add, nodeadmin:registerstatus, nodeadmin:reserveid,
nodeadmin:getreservation, nodeadmin:countreservations
```

**Admin config actions** — gate the `rmng_admin_config` table, granted only to
`SystemActor` and admin callers:
```
adminconfig:*, adminconfig:get, adminconfig:set
```

## Access Type -> Permissions

`GetGroupPermissions` maps a user's group access type to the actions granted on
that group:

| Access Type | Granted Actions |
| ----------- | --------------- |
| `primary`   | `group:*` on that group (full access, including delete and share) |
| `secondary` | `group:get`, `group:createsubgroup`, `group:listsubentities`, `group:update`, `group:updatesubgroup`, `group:deletesubgroup`, `group:listprimaryusers`, `group:getautomation`, `group:editautomation`, `group:deleteautomation` |
| `subentity` | `group:listsubentities`, `group:updatesubgroup`, `group:listprimaryusers` |

A secondary user gets **no** `group:delete`, `group:share`, or
`group:editnodes` — it can manage subgroups and automations but cannot add or
remove nodes, share the group onward, or delete it.

## Admin Check

Every admin endpoint establishes that the caller came through the admin Cognito
pool before doing anything else, and answers `403` if it did not. The check is on
the caller resolved from the request, not on a value in the request body.
Admin-pool membership is the whole privilege — there is no tier above it, so no
admin endpoint distinguishes one admin from another.
