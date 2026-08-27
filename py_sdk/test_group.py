# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
import warnings
from py_sdk.test_user import User

class Group:
    def __init__(self, user: User):
        self.user = user

    def create_group(self, group_name: str, capabilities: list = None) -> str:
        data = {"group_name": group_name}
        if capabilities:
            data["capabilities"] = capabilities
        create_group_data = json.dumps(data)
        create_group_response = self.user.make_api_request('POST', '/v1/groups', data=create_group_data)
        assert create_group_response.status_code == 201, f"Expected 201, but got {create_group_response.status_code}"

        create_group_data = create_group_response.json()
        assert "group_id" in create_group_data, "Response should contain group_id"
        print(f"User {self.user.username} created group {create_group_data['group_id']}")
        return create_group_data["group_id"]

    def create_matter_group(self, group_name: str) -> dict:
        """Create a group with Matter capability and return full response including Matter fields.

        Returns:
            dict: Full response containing group_id and matter object with fabric_id, root_ca, ipk, etc.
        """
        create_group_data = json.dumps({"group_name": group_name, "capabilities": ["matter"]})
        create_group_response = self.user.make_api_request('POST', '/v1/groups', data=create_group_data)
        assert create_group_response.status_code == 201, f"Expected 201, but got {create_group_response.status_code}"

        response_data = create_group_response.json()
        assert "group_id" in response_data, "Response should contain group_id"
        assert "matter" in response_data, "Response should contain 'matter' object for Matter-capable group"
        print(f"User {self.user.username} created Matter group {response_data['group_id']}")
        return response_data

    def add_group_capabilities(self, group_id: str, capabilities: list):
        """Enable capabilities on an existing group (POST /v1/groups/{id}/capabilities).

        Enabling ["matter"] converts the group into a Matter fabric. Returns the raw
        response so callers can assert on success (200) or on the rejection cases
        (400 for bad capabilities, 409 for already-enabled, 403 for non-owner).
        """
        print(f"User {self.user.username} enabling capabilities {capabilities} on group {group_id}")
        data = json.dumps({"capabilities": capabilities})
        return self.user.make_api_request('POST', f'/v1/groups/{group_id}/capabilities', data=data)

    def delete_group(self, group_id: str, warn_error=False):
        # Delete now requires the group to be empty (409 otherwise), so empty it
        # first. Tests asserting the 409 contract call the raw DELETE endpoint
        # directly, not this helper.
        for node_id in self._user_node_ids(self._get_group(group_id)):
            self.remove_node_from_group(group_id, node_id)
        for subgroup in (self._get_group(group_id) or {}).get("subgroups", []):
            self.delete_subgroup(group_id, subgroup["subgroup_id"])
        print(f"User {self.user.username} deleting group {group_id}")
        delete_group_response = self.user.make_api_request('DELETE', f'/v1/groups/{group_id}')
        if warn_error:
            if delete_group_response.status_code != 200:
                warnings.warn(f"Expected 2xx, but got {delete_group_response.status_code} while deleting group {group_id}")
        else:
            assert delete_group_response.status_code == 200, f"Expected 200, but got {delete_group_response.status_code} while deleting group {group_id}"

    def create_subgroup(self, group_id: str, subgroup_name: str) -> str:
        create_subgroup_data = json.dumps({"subgroup_name": subgroup_name})
        create_subgroup_response = self.user.make_api_request('POST', f'/v1/groups/{group_id}/subgroups', data=create_subgroup_data)
        assert create_subgroup_response.status_code == 201, f"Expected 201, but got {create_subgroup_response.status_code}"
        
        create_subgroup_data = create_subgroup_response.json()
        assert "subgroup_id" in create_subgroup_data, "Response should contain subgroup_id"
        print(f"User {self.user.username} created subgroup {create_subgroup_data['subgroup_id']} in group {group_id}")
        return create_subgroup_data["subgroup_id"]

    def delete_subgroup(self, group_id: str, subgroup_id: str, expected_status=200):
        # Delete now requires the subgroup to be empty (409 otherwise). On the
        # success path, empty it first. When a non-200 status is expected the
        # caller is asserting an error (e.g. 409 on a populated subgroup), so
        # leave it as-is.
        if expected_status == 200:
            for node_id in self._subgroup_user_node_ids(group_id, subgroup_id):
                self.remove_node_from_subgroup(group_id, subgroup_id, node_id)
        print(f"User {self.user.username} deleting subgroup {subgroup_id} from group {group_id}")
        delete_response = self.user.make_api_request('DELETE', f'/v1/groups/{group_id}/subgroups/{subgroup_id}')
        assert delete_response.status_code == expected_status, f"Expected {expected_status}, but got {delete_response.status_code} while deleting subgroup {subgroup_id}"
        return delete_response

    def _get_group(self, group_id: str):
        """Return the group dict from list_groups, or None if not present."""
        return next((g for g in self.list_groups().get("groups", []) if g["group_id"] == group_id), None)

    def _subgroup_user_node_ids(self, group_id: str, subgroup_id: str):
        """User node IDs currently in the given subgroup (child nodes excluded)."""
        group = self._get_group(group_id)
        if not group:
            return []
        subgroup = next((s for s in group.get("subgroups", []) if s["subgroup_id"] == subgroup_id), None)
        return self._user_node_ids(subgroup)

    @staticmethod
    def _user_node_ids(entity: dict):
        """node_ids on a group/subgroup minus child nodes ('--'), which
        the API refuses to remove directly (they're swept with their parent)."""
        if not entity:
            return []
        return [n for n in entity.get("node_ids", []) if "--" not in n]

    # empty_and_delete_* are retained as explicit aliases: delete_group /
    # delete_subgroup now empty the container first on their success path, so
    # these just make that intent obvious at call sites.
    def empty_and_delete_subgroup(self, group_id: str, subgroup_id: str, expected_status=200):
        return self.delete_subgroup(group_id, subgroup_id, expected_status=expected_status)

    def empty_and_delete_group(self, group_id: str, warn_error=False):
        return self.delete_group(group_id, warn_error=warn_error)

    def list_groups(self):
        list_groups_response = self.user.make_api_request('GET', '/v1/groups')
        assert list_groups_response.status_code == 200, f"Expected 200, but got {list_groups_response.status_code}"

        return list_groups_response.json()

    def list_group_users(self, group_id: str) -> dict:
        response = self.user.make_api_request('GET', f'/v1/groups/{group_id}/users')
        assert response.status_code == 200, f"Expected 200, but got {response.status_code}"
        return response.json()

    def list_group_users_raw(self, group_id: str):
        """Returns the raw response without asserting status code."""
        return self.user.make_api_request('GET', f'/v1/groups/{group_id}/users')

    def list_subgroup_users(self, group_id: str, subgroup_id: str) -> dict:
        response = self.list_subgroup_users_raw(group_id, subgroup_id)
        assert response.status_code == 200, f"Expected 200, but got {response.status_code}"
        return response.json()

    def list_subgroup_users_raw(self, group_id: str, subgroup_id: str):
        """Returns the raw response without asserting status code."""
        return self.user.make_api_request('GET', f'/v1/groups/{group_id}/subgroups/{subgroup_id}/users')

    def add_node_to_subgroup(self, group_id: str, subgroup_id: str, node_id: str):
        add_node_data = json.dumps({})
        add_node_response = self.user.make_api_request('PUT', f'/v1/groups/{group_id}/subgroups/{subgroup_id}/nodes/{node_id}', data=add_node_data)
        assert add_node_response.status_code == 200, f"Expected 200, but got {add_node_response.status_code}"
        assert add_node_response.json().get("message"), "Failed to add node to subgroup"
        return add_node_response

    def remove_node_from_subgroup(self, group_id: str, subgroup_id: str, node_id: str):
        remove_node_response = self.user.make_api_request('DELETE', f'/v1/groups/{group_id}/subgroups/{subgroup_id}/nodes/{node_id}')
        assert remove_node_response.status_code == 200, f"Expected 200, but got {remove_node_response.status_code}"
        assert remove_node_response.json().get("message"), "Failed to remove node from subgroup"

    def remove_node_from_group(self, group_id: str, node_id: str):
        """Remove a node completely from a group (including all subgroups)"""
        print(f"User {self.user.username} removing node {node_id} from group {group_id}")
        remove_node_response = self.user.make_api_request('DELETE', f'/v1/groups/{group_id}/nodes/{node_id}')
        assert remove_node_response.status_code == 200, f"Expected 200, but got {remove_node_response.status_code}"
        assert remove_node_response.json().get("message"), "Failed to remove node from group"
        return remove_node_response

    def verify_group_structure(self, group_id: str, expected_structure: dict):
        groups_data = self.list_groups()
        assert "groups" in groups_data, "Response should contain groups"
        
        group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
        assert group is not None, f"Group {group_id} not found in the list of groups"
        
        self._verify_group_structure_recursive(group, expected_structure)

    def _verify_group_structure_recursive(self, actual: dict, expected: dict):
        for key, value in expected.items():
            assert key in actual, f"Expected key '{key}' not found in actual structure"
            if isinstance(value, dict):
                self._verify_group_structure_recursive(actual[key], value)
            elif isinstance(value, list):
                assert isinstance(actual[key], list), f"Expected list for key '{key}', but got {type(actual[key])}"
                assert len(actual[key]) == len(value), f"Expected {len(value)} items for key '{key}', but got {len(actual[key])}"
                for item in value:
                    assert item in actual[key], f"Expected item '{item}' not found in list for key '{key}'"
            else:
                assert actual[key] == value, f"Expected '{value}' for key '{key}', but got '{actual[key]}'"

    def share_group(self, group_id: str, target_username: str, access_type: str):
        """Share a group. target_username is the invitee's email or E.164 phone —
        the identifier they sign in with. Removal still goes by user id, since
        that is what the member listing returns."""
        print("Sharing group", group_id, "with user", target_username, "and access type", access_type)
        share_data = json.dumps({
            "username": target_username,
            "access_type": access_type
        })
        share_response = self.user.make_api_request('POST', f'/v1/groups/{group_id}/sharing-requests', data=share_data)
        assert share_response.status_code == 201, f"Expected 201, but got {share_response.status_code}"
        response_data = share_response.json()
        assert response_data.get("request_id"), "Failed to share group"
        assert "request_id" in response_data, "Response should contain requestId"
        return share_response

    def share_group_by_qr_code(self, group_id: str, access_type: str):
        """Create a QR-code sharing request: no invitee is named, so whoever
        scans the returned request_id can claim it. Returns the request id."""
        print("Creating QR code sharing request for group", group_id, "with access type", access_type)
        share_data = json.dumps({"access_type": access_type})
        share_response = self.user.make_api_request('POST', f'/v1/groups/{group_id}/sharing-requests', data=share_data)
        assert share_response.status_code == 201, f"Expected 201, but got {share_response.status_code}: {share_response.text}"
        request_id = share_response.json().get("request_id")
        assert request_id, "Response should contain request_id"
        return request_id

    def unshare_group(self, group_id: str, target_user_id: str):
        print("Unsharing group", group_id, "with user", target_user_id)
        unshare_response = self.user.make_api_request('DELETE', f'/v1/groups/{group_id}/users/{target_user_id}')
        assert unshare_response.status_code == 200, f"Expected 200, but got {unshare_response.status_code}"

    def leave_group(self, group_id: str):
        return self.unshare_group(group_id, "me")

    def share_subgroup(self, group_id: str, subgroup_id: str, target_username: str):
        """target_username is the invitee's email or E.164 phone — see share_group."""
        print("Sharing subgroup", subgroup_id, "with user", target_username)
        share_data = json.dumps({
            "username": target_username
        })
        share_response = self.user.make_api_request('POST', f'/v1/groups/{group_id}/subgroups/{subgroup_id}/sharing-requests', data=share_data)
        assert share_response.status_code == 201, f"Expected 201, but got {share_response.status_code}"
        response_data = share_response.json()
        assert response_data.get("request_id"), "Failed to share subgroup"
        assert "request_id" in response_data, "Response should contain requestId"
        return response_data["request_id"]

    def share_subgroup_by_qr_code(self, group_id: str, subgroup_id: str):
        """Create a QR-code sharing request for a subgroup — see share_group_by_qr_code."""
        print("Creating QR code sharing request for subgroup", subgroup_id)
        share_response = self.user.make_api_request('POST', f'/v1/groups/{group_id}/subgroups/{subgroup_id}/sharing-requests', data=json.dumps({}))
        assert share_response.status_code == 201, f"Expected 201, but got {share_response.status_code}: {share_response.text}"
        request_id = share_response.json().get("request_id")
        assert request_id, "Response should contain request_id"
        return request_id

    def unshare_subgroup(self, group_id: str, subgroup_id: str, target_user_id: str):
        print("Unsharing subgroup", subgroup_id, "with user", target_user_id)
        unshare_response = self.user.make_api_request('DELETE', f'/v1/groups/{group_id}/subgroups/{subgroup_id}/users/{target_user_id}')
        assert unshare_response.status_code == 200, f"Expected 200, but got {unshare_response.status_code}"

    def leave_subgroup(self, group_id: str, subgroup_id: str):
        return self.unshare_subgroup(group_id, subgroup_id, "me")

    def update_group(self, group_id: str, new_group_name: str):
        print(f"User {self.user.username} updating group {group_id} to '{new_group_name}'")
        update_data = json.dumps({"group_name": new_group_name})
        update_response = self.user.make_api_request('PATCH', f'/v1/groups/{group_id}', data=update_data)
        assert update_response.status_code == 200, f"Expected 200, but got {update_response.status_code}"
        assert update_response.json().get("message"), "Failed to update group"
        return update_response

    def update_subgroup(self, group_id: str, subgroup_id: str, new_subgroup_name: str, expected_status=200):
        print(f"User {self.user.username} updating subgroup {subgroup_id} in group {group_id} to '{new_subgroup_name}'")
        update_data = json.dumps({"subgroup_name": new_subgroup_name})
        update_response = self.user.make_api_request('PATCH', f'/v1/groups/{group_id}/subgroups/{subgroup_id}', data=update_data)
        assert update_response.status_code == expected_status, f"Expected {expected_status}, but got {update_response.status_code} while updating subgroup {subgroup_id}"
        if expected_status == 200:
            assert update_response.json().get("message"), "Failed to update subgroup"
        return update_response
