# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from py_sdk.test_user import User
from test.itest.email_utils import generate_random_email, generate_test_password
from test.itest.conftest import (
    connect_device_with_retry,
    REGION,
    IDENTITY_POOL_ID,
    API_GATEWAY_URL,
    USER_API_GATEWAY_URL,
    IOT_ENDPOINT,
)
import pytest
import time


@pytest.mark.parametrize("basic_ingest", [True, False], ids=["basic_ingest", "direct_topic"])
def test_timeseries_comprehensive(associated_device, basic_ingest):
    """
    Comprehensive integration test for timeseries functionality.
    Tests the complete timeseries workflow and all REST APIs:
    1. Device publishes timeseries data to MQTT topic
    2. IoT Rule processes the data and stores it in DynamoDB
    3. Comprehensive testing of all REST APIs (raw, latest, aggregates)
    4. Validates topic format, data integrity, and all window types
    5. Tests pagination, unauthorized access, and edge cases
    """
    device, group_id, test_user1, user1_group_api = associated_device
    device_thing_name = device.node_thing_name

    print("🔄 Starting comprehensive timeseries test...")

    # Ensure device is connected and has group info
    assert connect_device_with_retry(device), "Failed to connect device"

    # Get group info to populate group_id for topic construction
    device.get_group_info()
    assert hasattr(device, 'group_id') and device.group_id, "Device should have group_id after get_group_info"

    # Validate the topic name that will be used
    expected_topic = f"rainmaker/nodes/{device_thing_name}/ts/{group_id}"
    if hasattr(device, 'subgroup_ids') and device.subgroup_ids:
        subgroup_str = ",".join(device.subgroup_ids)
        expected_topic += f"-{subgroup_str}"

    print(f"Expected timeseries topic: {expected_topic}")

    # PART 1: PUBLISH COMPREHENSIVE TEST DATA
    print("📊 Publishing comprehensive test data...")

    # Base timestamp: October 27, 2024, 00:00:00 UTC
    oct_27_base = 1729987200  # 2024-10-27 00:00:00 UTC

    # Basic test data points for end-to-end validation
    # Note: temperature is excluded here since we use comprehensive temperature data below
    basic_test_data = [
        {"name": "humidity", "dt": "float", "value": 65.2, "cumulative": False},
        {"name": "power", "dt": "bool", "value": True, "cumulative": False},
        {"name": "count", "dt": "int", "value": 42, "cumulative": False},
        {"name": "energy", "dt": "float", "value": 1500.75, "cumulative": True}
    ]

    # Comprehensive temperature data for REST API testing AND pagination testing
    # This will give us exactly 50 data points total with guaranteed unique timestamps
    # Organized to properly test different aggregate windows (hourly, daily, weekly, monthly)
    temperature_test_data = []

    # Use non-overlapping time ranges to prevent timestamp collisions
    # while maintaining proper distribution for aggregate testing

    # 10 monthly points: 400-310 days back (10-day intervals)
    # This tests monthly aggregates with good distribution
    for i in range(10):
        days_back = 400 - (i * 10)  # 400, 390, 380, ..., 310 days back
        timestamp = oct_27_base - (days_back * 86400)
        temp_value = 15.0 + i * 0.3
        temperature_test_data.append({'ts': timestamp, 'temp': temp_value})

    # 15 weekly points: 300-195 days back (7-day intervals)
    # This tests weekly aggregates with proper weekly spacing
    for i in range(15):
        days_back = 300 - (i * 7)  # 300, 293, 286, ..., 202 days back
        timestamp = oct_27_base - (days_back * 86400)
        temp_value = 18.0 + i * 0.2
        temperature_test_data.append({'ts': timestamp, 'temp': temp_value})

    # 15 daily points: 190-176 days back (1-day intervals)
    # This tests daily aggregates with consecutive days
    for i in range(15):
        days_back = 190 - i  # 190, 189, 188, ..., 176 days back
        timestamp = oct_27_base - (days_back * 86400)
        temp_value = 21.0 + i * 0.1
        temperature_test_data.append({'ts': timestamp, 'temp': temp_value})

    # 10 hourly points: October 27, 2024 (1-hour intervals)
    # This tests hourly aggregates with consecutive hours
    for i in range(10):
        hours_forward = i  # 0, 1, 2, ..., 9 hours from base
        timestamp = oct_27_base + (hours_forward * 3600)
        temp_value = 22.5 + i * 0.1
        temperature_test_data.append({'ts': timestamp, 'temp': temp_value})

    # Publish basic test data first
    print("📊 Publishing basic test data...")
    basic_published_timestamps = []

    for data_point in basic_test_data:
        print(f"Publishing {data_point['name']}: {data_point['value']}")
        success = device.publish_timeseries_data(
            k=data_point["name"],
            data_type=data_point["dt"],
            value=data_point["value"],
            cumulative=data_point["cumulative"],
            basic_ingest=basic_ingest,
        )
        assert success, f"Failed to publish {data_point['name']} data"
        basic_published_timestamps.append(int(time.time() * 1000))
        time.sleep(1)

    # Publish comprehensive temperature data (50 points total)
    # This will be used for both aggregates testing AND pagination testing
    print(f"📊 Publishing comprehensive temperature data ({len(temperature_test_data)} points)...")
    temperature_test_data.sort(key=lambda x: x['ts'])

    for i, data_point in enumerate(temperature_test_data):
        device.publish_timeseries_data(
            k="temperature",
            data_type="float",
            value=data_point['temp'],
            timestamp=data_point['ts'],
            basic_ingest=basic_ingest,
        )
        if (i + 1) % 10 == 0:
            print(f"Published {i + 1}/{len(temperature_test_data)} temperature data points")

    print("✅ All test data published. Waiting for stream processor...")
    time.sleep(25)  # Wait for stream processor to process all data

    # PART 2: BASIC END-TO-END VALIDATION
    print("🔍 Part 2: Basic end-to-end validation...")

    def validate_basic_timeseries_data():
        """Validate basic timeseries data was stored correctly"""
        for i, data_point in enumerate(basic_test_data):
            path = data_point["name"]
            data_type = data_point["dt"]
            expected_value = data_point["value"]

            # Query the timeseries API
            response = test_user1.get_timeseries_data(
                group_id=group_id,
                node_id=device_thing_name,
                key=path,
                data_type=data_type
            )

            # Validate response structure
            assert response is not None, f"Should receive response for {path}"
            assert "data" in response, f"Response should contain data for {path}"

            data_entries = response["data"]
            assert len(data_entries) > 0, f"Should have at least 1 data entry for {path}"

            # Find the entry we just published (should be the most recent)
            latest_entry = data_entries[0]

            # Validate the data
            assert latest_entry["key"] == path, f"Path should match"
            assert latest_entry["dt"] == data_type, f"Data type should match"
            assert latest_entry["value"] == expected_value, f"Value should match for {path}"
            assert latest_entry["cumulative"] == data_point["cumulative"], f"Cumulative flag should match"

            # Validate timestamp is recent (within last 30 seconds for basic data)
            if i < len(basic_published_timestamps):
                entry_timestamp = latest_entry["ts"]
                time_diff = abs(entry_timestamp - basic_published_timestamps[i])
                assert time_diff < 30000, f"Timestamp should be recent for {path}"

            print(f"✅ Validated {path} data: {expected_value}")

    # Use retry logic for data validation (IoT Rule processing is asynchronous)
    def retry_validation(validation_func, max_retries=10, delay=3):
        """Retry validation with exponential backoff"""
        for attempt in range(max_retries):
            try:
                validation_func()
                return True
            except Exception as e:
                if attempt < max_retries - 1:
                    print(f"⏳ Validation attempt {attempt + 1} failed: {str(e)}")
                    print(f"   Retrying in {delay} seconds...")
                    time.sleep(delay)
                    delay = min(delay * 1.5, 10)
                else:
                    raise e
        return False

    # Execute basic validation with retry
    print("🔄 Executing basic timeseries validation with retry logic...")
    assert retry_validation(validate_basic_timeseries_data), "Basic timeseries data validation failed after retries"

    # PART 3: COMPREHENSIVE REST API TESTING
    print("🔍 Part 3: Comprehensive REST API testing...")

    # Test API Info
    print("3.1 Testing API Info...")
    response = test_user1.get_node_timeseries_api_info(group_id, device_thing_name)
    assert response['service'] == 'timeseries'
    assert 'parameters' in response
    assert 'examples' in response
    print("✓ API Info test passed")

    # Test Raw Data APIs
    print("3.2 Testing Raw Data APIs...")

    # Basic raw data - use a start_time (in ms) that covers all test data (400 days back from base)
    start_time_basic = (oct_27_base - (400 * 86400)) * 1000  # ms; 400 days before base to get all data
    response = test_user1.get_node_timeseries_raw(group_id, device_thing_name, "temperature", "float", start_time=start_time_basic)
    assert 'data' in response
    assert len(response['data']) > 0
    print("✓ Basic raw data test passed")

    # Raw data with page_size
    response = test_user1.get_node_timeseries_raw(group_id, device_thing_name, "temperature", "float", start_time=start_time_basic, page_size=10)
    assert 'data' in response
    assert len(response['data']) <= 10
    print("✓ Raw data with page_size test passed")

    # Raw data with time range (start_time/end_time in ms)
    start_time = oct_27_base * 1000
    end_time = (oct_27_base + (12 * 3600)) * 1000
    response = test_user1.get_node_timeseries_raw(group_id, device_thing_name, "temperature", "float",
                                                  start_time=start_time, end_time=end_time)
    assert 'data' in response
    assert len(response['data']) > 0
    print(f"✓ Raw data with time range test passed - found {len(response['data'])} data points")

    # Test Latest Data API
    print("3.3 Testing Latest Data API...")

    # Test all basic parameters
    for data_point in basic_test_data:
        response = test_user1.get_node_timeseries_latest(group_id, device_thing_name, data_point["name"], data_point["dt"])
        assert 'data' in response
        # Latest endpoint now returns a single object, not an array
        assert isinstance(response['data'], dict)
        latest_data = response['data']
        assert 'ts' in latest_data
        assert 'value' in latest_data
        assert latest_data['key'] == data_point["name"]
        print(f"✓ Latest {data_point['name']} data test passed - value: {latest_data['value']}")

    # Test temperature latest data (from comprehensive dataset)
    response = test_user1.get_node_timeseries_latest(group_id, device_thing_name, "temperature", "float")
    assert 'data' in response
    # Latest endpoint now returns a single object, not an array
    assert isinstance(response['data'], dict)
    latest_data = response['data']
    assert 'ts' in latest_data
    assert 'value' in latest_data
    assert latest_data['key'] == "temperature"
    print(f"✓ Latest temperature data test passed - value: {latest_data['value']}")

    # Test Current Aggregates APIs
    print("3.4 Testing Current Aggregates APIs...")

    # All current aggregates - use daily window as default
    response = test_user1.get_node_timeseries_current_aggregates(group_id, device_thing_name, "temperature", "float", window="daily")
    assert 'aggregates' in response
    assert isinstance(response['aggregates'], list)
    assert len(response['aggregates']) == 1
    print("✓ All current aggregates test passed")

    # Test all window types
    for window in ["hourly", "daily", "weekly", "monthly"]:
        response = test_user1.get_node_timeseries_current_aggregates(group_id, device_thing_name, "temperature", "float", window=window)
        assert 'aggregates' in response
        assert isinstance(response['aggregates'], list)
        assert len(response['aggregates']) == 1
        print(f"✓ Current {window} aggregates test passed")

    # Test Historical Aggregates APIs
    print("3.5 Testing Historical Aggregates APIs...")

    # Historical daily aggregates for specific date
    response = test_user1.get_node_timeseries_historical_aggregates(group_id, device_thing_name, "temperature", "float",
                                                                   window="daily", date="2024-10-15")
    assert 'aggregates' in response
    assert isinstance(response['aggregates'], list)
    assert len(response['aggregates']) == 1
    print("✓ Historical daily aggregates for specific date test passed")

    # Historical hourly aggregates for specific date
    response = test_user1.get_node_timeseries_historical_aggregates(group_id, device_thing_name, "temperature", "float",
                                                                   window="hourly", date="2024-10-27")
    assert 'aggregates' in response
    assert isinstance(response['aggregates'], list)
    assert len(response['aggregates']) == 1
    print("✓ Historical hourly aggregates for specific date test passed")

    # Test hour-level specification
    response = test_user1.get_node_timeseries_historical_aggregates(group_id, device_thing_name, "temperature", "float",
                                                                   window="hourly", date="2024-10-27T03")
    assert 'aggregates' in response
    assert isinstance(response['aggregates'], list)
    assert len(response['aggregates']) == 1
    print("✓ Historical hourly aggregates with hour specification test passed")

    # Test Historical Aggregates Range APIs
    print("3.6 Testing Historical Aggregates Range APIs...")

    # Historical daily aggregates for date range
    response = test_user1.get_node_timeseries_historical_aggregates_range(group_id, device_thing_name, "temperature", "float",
                                                                         window="daily", start_date="2024-10-01", end_date="2024-10-15")
    assert 'aggregates' in response
    print("✓ Historical daily aggregates for date range test passed")

    # Historical hourly aggregates with hour specification
    response = test_user1.get_node_timeseries_historical_aggregates_range(group_id, device_thing_name, "temperature", "float",
                                                                         window="hourly", start_date="2024-10-27T02", end_date="2024-10-27T06")
    assert 'aggregates' in response
    print("✓ Historical hourly aggregates range with hour specification test passed")

    # PART 4: PAGINATION TESTING
    print("🔍 Part 4: Comprehensive pagination testing...")

    # Basic pagination test
    first_param = basic_test_data[0]
    paginated_response = test_user1.get_timeseries_data(
        group_id=group_id,
        node_id=device_thing_name,
        key=first_param["name"],
        data_type=first_param["dt"],
        page_size=1
    )

    if paginated_response:
        assert "data" in paginated_response, "Paginated response should contain data"
        assert len(paginated_response["data"]) <= 1, "Should respect page_size parameter"

        if paginated_response.get("next_key", "") != "":
            assert "next_key" in paginated_response, "Should have next_key when there are more results"

            next_response = test_user1.get_timeseries_data(
                group_id=group_id,
                node_id=device_thing_name,
                key=first_param["name"],
                data_type=first_param["dt"],
                page_size=1,
                start_key=paginated_response["next_key"]
            )

            if next_response:
                assert "data" in next_response, "Next page response should contain data"
                print("✅ Basic pagination with next_key works correctly")
            else:
                print("⚠️  Next page returned no data (acceptable if only one record exists)")
        else:
            print("✅ Single page response (no pagination needed)")

    # Multi-page pagination test with temperature data
    def test_comprehensive_pagination():
        """Test complete pagination workflow with temperature data (50 points)"""
        all_collected_data = []
        start_key = None
        page_count = 0

        while True:
            page_count += 1
            print(f"   Fetching page {page_count}...")

            response = test_user1.get_timeseries_data(
                group_id=group_id,
                node_id=device_thing_name,
                key="temperature",
                data_type="float",
                page_size=3,
                start_key=start_key
            )

            assert response is not None, f"Should receive response for page {page_count}"
            assert "data" in response, f"Page {page_count} should contain data"

            page_data = response["data"]
            has_more = response.get("next_key", "") != ""

            print(f"   Page {page_count}: {len(page_data)} items, has_more={has_more}")

            assert len(page_data) <= 3, f"Page {page_count} should not exceed page_size of 3"
            assert len(page_data) > 0, f"Page {page_count} should have at least 1 item"

            all_collected_data.extend(page_data)

            if has_more:
                assert "next_key" in response, f"Page {page_count} should have next_key"
                assert response["next_key"] != "", f"Page {page_count} next_key should not be empty"
                start_key = response["next_key"]
                assert page_count < 20, "Too many pages - possible infinite loop"
            else:
                assert "next_key" not in response or response.get("next_key") == "", f"Last page should not have next_key"
                break

        print(f"✅ Collected {len(all_collected_data)} items across {page_count} pages")

        # With exactly 50 temperature data points, we should have exactly 50 entries
        assert len(all_collected_data) == 50, f"Should have exactly 50 temperature entries, got {len(all_collected_data)}"

        # Verify data is in correct order (latest first)
        for i in range(len(all_collected_data) - 1):
            current_timestamp = all_collected_data[i]["ts"]
            next_timestamp = all_collected_data[i + 1]["ts"]
            assert current_timestamp > next_timestamp, f"Data should be in descending timestamp order"

        # Verify no duplicate entries
        timestamps = [item["ts"] for item in all_collected_data]
        assert len(timestamps) == len(set(timestamps)), "Should not have duplicate timestamps"

        # Verify all entries are temperature data
        for item in all_collected_data:
            assert item["key"] == "temperature", "All entries should be temperature data"
            assert item["dt"] == "float", "All entries should be float data type"

        print("✅ Pagination data validation completed")
        return all_collected_data

    # Execute comprehensive pagination test with retry
    def retry_pagination_test(max_retries=3, delay=2):
        """Retry pagination test in case IoT rule processing is slow"""
        for attempt in range(max_retries):
            try:
                return test_comprehensive_pagination()
            except Exception as e:
                if attempt < max_retries - 1:
                    print(f"⏳ Pagination test attempt {attempt + 1} failed: {str(e)}")
                    print(f"   Retrying in {delay} seconds...")
                    time.sleep(delay)
                    delay *= 1.5
                else:
                    raise e

    collected_pagination_data = retry_pagination_test()
    print("✅ Comprehensive pagination testing completed")

    # Test pagination with historical aggregates
    response = test_user1.get_node_timeseries_historical_aggregates_range(group_id, device_thing_name, "temperature", "float",
                                                                         window="daily", start_date="2024-10-01",
                                                                         end_date="2024-10-31", page_size=10)
    assert 'aggregates' in response
    print("✓ Historical aggregates pagination test passed")

    # PART 5: UNAUTHORIZED ACCESS TESTING
    print("🔍 Part 5: Testing unauthorized access...")

    from test.itest.conftest import END_USER_POOL_ID
    # register_user_via_lambda force-creates the user (no emailed code is ever read), so a
    # random no-Mailosaur address suffices — see generate_random_email's docstring.
    unauthorized_email = generate_random_email()
    test_user2 = User(unauthorized_email, generate_test_password(), REGION, IDENTITY_POOL_ID, API_GATEWAY_URL, USER_API_GATEWAY_URL, IOT_ENDPOINT, end_user_pool_id=END_USER_POOL_ID)
    test_user2.register_user_via_lambda(email=test_user2.username, password=test_user2.password)
    test_user2.get_aws_credentials()

    try:
        # This should fail with unauthorized error
        unauthorized_response = test_user2.get_timeseries_data(
            group_id=group_id,
            node_id=device_thing_name,
            key="temperature",
            data_type="float"
        )
        assert False, "Unauthorized user should not be able to access timeseries data"
    except Exception as e:
        assert "unauthorized" in str(e).lower() or "403" in str(e) or "401" in str(e), f"Should get unauthorized error, got: {str(e)}"
        print("✅ Unauthorized access properly blocked")

    # Cleanup
    device.disconnect()

    print("🎉 Comprehensive timeseries test completed successfully!")
    print("📋 Test Summary:")
    print("   - End-to-end workflow validation ✅")
    print("   - Basic and comprehensive data publishing ✅")
    print("   - Raw data APIs ✅")
    print("   - Latest data APIs ✅")
    print("   - Current aggregates (all window types) ✅")
    print("   - Historical aggregates (single date) ✅")
    print("   - Historical aggregates (date ranges) ✅")
    print("   - Hour-level specification ✅")
    print("   - Comprehensive pagination ✅")
    print("   - Unauthorized access blocking ✅")


def test_timeseries_cross_tenant_read_denied(two_tenants):
    """A must not read B's node time-series data (sensor-history exfiltration)."""
    tenant_a, tenant_b = two_tenants
    user_a = tenant_a["user"]
    node_b = tenant_b["node_id"]

    r1 = user_a.make_api_request(
        "GET", f"/v1/groups/{tenant_a['group_id']}/nodes/{node_b}/timeseries/raw",
        params={"key": "Temperature", "data_type": "float"})
    assert r1.status_code >= 400, (
        f"Read foreign node timeseries via own-group path returned {r1.status_code}: {r1.text[:150]}"
    )
    r2 = user_a.make_api_request(
        "GET", f"/v1/groups/{tenant_b['group_id']}/nodes/{node_b}/timeseries/raw",
        params={"key": "Temperature", "data_type": "float"})
    assert r2.status_code >= 400, (
        f"Read foreign node timeseries via foreign-group path returned {r2.status_code}: {r2.text[:150]}"
    )
