// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
The node_id_reservations table binds a claim key to the node ID the cloud
assigned to it (see misc/specs/assisted-claiming.md). It is the record
that makes certificate identity server-determined: claim-verify reads the node
ID from here, never from anything the caller submits.

Table Name: rmng-node-id-reservations
Primary Key: claimant_id (Partition Key), mac_addr (Sort Key)

The claimant is the partition key so quota counting — "how many reservations
does this caller hold" — is a base-table Query on the partition, with no GSI to
provision. That was a deliberate schema choice: a GSI adds minutes to the
table's deploy (DynamoDB backfills it) and write amplification for its lifetime,
for a single count query. Making the claimant the partition instead removes the
index entirely, and lets the count run with ConsistentRead so a burst of claims
cannot slip past the quota through an eventually-consistent under-count.

This only stays free of a hot partition because every claimant value is
high-cardinality: a real caller's user ID under user_authenticated, or the
per-MAC-sharded sentinel under device_attested (see claim.NewKey). A fixed
sentinel would concentrate every device_attested write onto one partition key.

Schema:
- claimant_id (String): Partition key, the claiming caller's resolved internal
  user ID (or a per-MAC sentinel under device_attested, which has no caller).
  There is no tenancy concept here — the caller dimension exists solely to stop
  one caller's claim from replacing the certificate on another caller's
  in-service device.
- mac_addr (String): Sort key, normalized device MAC address (separators
  stripped, uppercase) so one device resolves to one reservation regardless of
  the caller's formatting convention.
- node_id (String): Cloud-assigned node identifier — a canonical hyphenated
  UUIDv4, which doubles as the Thing name, MQTT client ID and certificate CN. A
  node that joins a Matter fabric has its Matter Node ID derived from this; the
  node_id is not itself a Matter Node ID.
- created_at (Number): Unix timestamp of reservation creation

The table has no GSIs, so a fresh CreateTable provisions it in one call with no
backfill wait.

Query Patterns:
1. Get reservation by (claimant_id, mac_addr): idempotent claim-initiate lookups.
2. Count by claimant_id: quota enforcement, a base-table partition Query.

Access Control:
- NodeAdminReserveID to create a reservation (claim-initiate lambda). Resource: mac_addr.
- NodeAdminGetReservation to read one reservation (claim-initiate and claim-verify). Resource: mac_addr.
- NodeAdminCountReservations to count a claimant's reservations for the quota
  (claim-initiate). Resource: claimant_id.
*/

package node_id_reservation_db

import (
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
)

const (
	// Table name — must byte-match TABLE_NAMES['NODE_ID_RESERVATIONS'] in
	// src/base_res_constants.py.
	NodeIDReservationsTable = "rmng-node-id-reservations"

	// Key column names. claimant_id is the partition key so the quota count is
	// a base-table Query with no GSI (see the package doc comment).
	reservationsHashKey  = "claimant_id"
	reservationsRangeKey = "mac_addr"
)

type NodeIDReservationsDB struct {
	espdynamodb.EspDB
}

func NewNodeIDReservationsDB(ctx *rmngctx.RmngContext) *NodeIDReservationsDB {
	return &NodeIDReservationsDB{EspDB: espdynamodb.NewEspDB(ctx)}
}

type ReservationEntry struct {
	MacAddr    string `dynamodbav:"mac_addr"`
	ClaimantID string `dynamodbav:"claimant_id"`
	NodeID     string `dynamodbav:"node_id,omitempty"`
	// CAID names the CA that signed the node's current certificate. Written at
	// issuance, not at reservation, so it is empty between initiate and the
	// first verify. Recorded so a second CA can be introduced and the first
	// retired without ambiguity about which chain a given node trusts.
	CAID      string `dynamodbav:"ca_id,omitempty"`
	CreatedAt int64  `dynamodbav:"created_at,omitempty"`
}

func (r *ReservationEntry) GetHKey() string {
	return reservationsHashKey
}

func (r *ReservationEntry) GetRKey() string {
	return reservationsRangeKey
}

// CreateReservation stores a new {mac_addr, claimant_id} -> node_id reservation.
// The underlying create is conditional on the item not existing, so concurrent
// calls for the same pair cannot overwrite an earlier reservation.
func (db *NodeIDReservationsDB) CreateReservation(entry ReservationEntry) error {
	if err := db.DB.IsAuthorized(utils.NodeAdminReserveID, entry.MacAddr); err != nil {
		return err
	}

	entry.CreatedAt = time.Now().Unix()

	if err := db.DbCreateItem(NodeIDReservationsTable, &entry); err != nil {
		return rmerror.NewRMError(err, "failed to create node ID reservation")
	}
	return nil
}

// GetReservation returns the reservation for a {mac_addr, claimant_id} pair, or
// nil when no reservation exists.
func (db *NodeIDReservationsDB) GetReservation(macAddr string, claimantID string) (*ReservationEntry, error) {
	if err := db.DB.IsAuthorized(utils.NodeAdminGetReservation, macAddr); err != nil {
		return nil, err
	}

	entry := &ReservationEntry{}
	err := db.DbGetItem(NodeIDReservationsTable, &ReservationEntry{MacAddr: macAddr, ClaimantID: claimantID}, entry)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get node ID reservation")
	}
	if entry.NodeID == "" {
		return nil, nil
	}
	return entry, nil
}

// SetCAID records which CA signed the node's current certificate.
//
// Called at issuance rather than at reservation, because the CA is only known
// once a certificate is actually produced. The update is conditional on the
// reservation existing, so it cannot resurrect a reservation that was removed
// between initiate and verify.
func (db *NodeIDReservationsDB) SetCAID(macAddr string, claimantID string, caID string) error {
	if err := db.DB.IsAuthorized(utils.NodeAdminReserveID, macAddr); err != nil {
		return err
	}
	if caID == "" {
		return rmerror.NewRMError(nil, "ca_id is empty")
	}

	_, err := db.DbUpdateItem(espdynamodb.DbUpdateItemInput{
		TableName: NodeIDReservationsTable,
		Query:     &ReservationEntry{MacAddr: macAddr, ClaimantID: claimantID},
		Update:    expression.Set(expression.Name("ca_id"), expression.Value(caID)),
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to record issuing CA on reservation")
	}
	return nil
}

// CountReservationsForClaimant returns how many node-ID reservations a
// claimant holds. Used for per-claimant quota enforcement.
//
// claimant_id is the table's partition key, so this is a base-table Query on
// the partition — no GSI. Run with a strongly consistent read so a burst of
// concurrent claims cannot under-count and let a caller slip past the quota.
func (db *NodeIDReservationsDB) CountReservationsForClaimant(claimantID string) (int, error) {
	// NodeAdminCountReservations (not NodeAdminGetReservation) because this is a
	// claimant-scoped operation — it counts every reservation the claimant holds
	// across all their devices, so the resource is the claimant_id, and there is
	// no single MAC to authorize against as Create/Get/SetCAID do. A distinct
	// action lets claim-initiate grant it scoped to the one claimant rather than
	// widening the per-device grant to "*".
	if err := db.DB.IsAuthorized(utils.NodeAdminCountReservations, claimantID); err != nil {
		return 0, err
	}

	keyCondition := expression.Key(reservationsHashKey).Equal(expression.Value(claimantID))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	if err != nil {
		return 0, rmerror.NewRMError(err, "failed to build reservation count expression")
	}

	count, err := db.DbQueryCountLoop(espdynamodb.DbQueryCountInput{
		TableName:      NodeIDReservationsTable,
		ConsistentRead: true,
		Expr:           expr,
	})
	if err != nil {
		return 0, rmerror.NewRMError(err, "failed to count claimant reservations")
	}
	return int(count), nil
}
