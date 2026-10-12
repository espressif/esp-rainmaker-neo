// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
The node_reg_reqs table stores information about bulk node registration requests:

Table Name: node_reg_reqs
Primary Key: request_id (Partition Key)

Schema:
- request_id (String): Partition key, unique identifier for the request (ECS task ID)
- user_id (String): ID of the user who initiated the request
- status (String): Current status of the registration request (started/data_loaded/completed)
- cert_file_s3_path (String): S3 path to the certificate file
- failed_file_s3_path (String, optional): S3 path of the cert-bearing failed-rows CSV, written at end-of-job when failures occurred
- admin_group_names (List[String]): List of admin group names for the nodes
- tags (List[String]): Tags to be applied to the nodes
- total_nodes (Number): Total number of nodes to be registered
- success_count (Number, optional): Number of successfully registered nodes
- failed_count (Number, optional): Number of failed node registrations
- created_at (Number): Unix timestamp of request creation
- last_updated_at (Number): Unix timestamp of last update
- message (String, optional): Additional status message or error details

Status Values:
- started: Initial state when request is created
- data_loaded: Data has been loaded and validated
- completed: Registration process has completed (success or failure)

Query Patterns:
1. Get registration request by request_id:
   - Query by request_id to get registration status and details
   - Used for tracking progress and debugging

Access Control:
- Only users with NodeAdminAdd permission can create requests
- Only users with NodeAdminRegisterStatus permission can view requests
- Updates are restricted to the user who created the request
*/

package node_reg_req_db

import (
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

const (
	// Table name
	NodeRegReqsTable = "rmng-node-reg-reqs"

	// Key column names
	nodeRegRequestsHashKey = "request_id"

	// GSI for listing all requests
	NodeRegReqsListIndex       = "rmng-node-reg-reqs-list"
	nodeRegRequestsGSIHashKey  = "gsi_pk"
	nodeRegRequestsGSIRangeKey = "created_at"
	nodeRegRequestsGSIPK       = "ALL" // Fixed partition key for the list GSI

	// Column names
	nodeRegRequestsUserIDCol = "user_id"
)

type NodeRegRequestsDB struct {
	Ctx      *rmngctx.RmngContext
	requests *ddbu.Table[NodeRegRequestsEntry]
}

func NewNodeRegRequestsDB(ctx *rmngctx.RmngContext) *NodeRegRequestsDB {
	return &NodeRegRequestsDB{
		Ctx: ctx,
		requests: ddbu.NewTable[NodeRegRequestsEntry](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:    NodeRegReqsTable,
			HashKey: nodeRegRequestsHashKey,
			Indexes: map[string]ddbu.IndexKeys{
				NodeRegReqsListIndex: {HashKey: nodeRegRequestsGSIHashKey, RangeKey: nodeRegRequestsGSIRangeKey},
			},
		}),
	}
}

type NodeRegRequestsEntry struct {
	RequestID            string   `dynamodbav:"request_id"`        //ECS task id
	GSIPK                string   `dynamodbav:"gsi_pk,omitempty"`  // Fixed partition key for list GSI
	UserID               string   `dynamodbav:"user_id,omitempty"` //Just for logging who started the request
	Status               string   `dynamodbav:"status,omitempty"`
	JobType              string   `dynamodbav:"job_type,omitempty"` // "register" | "update". Empty == "register" for backward compat with rows written before this field existed.
	CertFileS3Path       string   `dynamodbav:"cert_file_s3_path,omitempty"`
	FailedFileS3Path     string   `dynamodbav:"failed_file_s3_path,omitempty"` // S3 key of the cert-bearing failed-rows CSV; set by the container at end-of-job when failures occurred.
	AdminGroupNames      []string `dynamodbav:"admin_group_names,omitempty"`
	AdminParentGroupName string   `dynamodbav:"admin_parent_group_name,omitempty"`
	Tags                 []string `dynamodbav:"tags,omitempty"`
	TotalCount           int      `dynamodbav:"total_nodes,omitempty"`
	SuccessCount         *int     `dynamodbav:"success_count,omitempty"`
	FailedCount          *int     `dynamodbav:"failed_count,omitempty"`
	CreatedAt            int64    `dynamodbav:"created_at,omitempty"`
	LastUpdatedAt        int64    `dynamodbav:"last_updated_at,omitempty"`
	Message              string   `dynamodbav:"message,omitempty"`
}

// bulk node register job status
const (
	NODE_REG_STATUS_REQUESTED   = "requested"
	NODE_REG_STATUS_STARTED     = "started"
	NODE_REG_STATUS_DATA_LOADED = "data_loaded"
	NODE_REG_STATUS_COMPLETED   = "completed"
)

func IsValidNodeRegStatus(status string) error {
	if status == NODE_REG_STATUS_REQUESTED || status == NODE_REG_STATUS_STARTED || status == NODE_REG_STATUS_COMPLETED || status == NODE_REG_STATUS_DATA_LOADED {
		return nil
	}
	return rmerror.NewRMError(errors.New("invalid node registration status"), "invalid node registration status "+status)
}

// Job type discriminates between the two bulk operation flows that share
// node_reg_reqs as their tracking table. Empty string is treated as "register"
// so rows written before this field existed remain interpretable.
const (
	NODE_REG_JOB_TYPE_REGISTER = "register"
	NODE_REG_JOB_TYPE_UPDATE   = "update"
)

// JobTypeOrDefault returns the row's job_type, falling back to "register"
// when the field is empty (older rows or callers that don't yet stamp it).
func JobTypeOrDefault(jobType string) string {
	if jobType == "" {
		return NODE_REG_JOB_TYPE_REGISTER
	}
	return jobType
}

func IsValidJobType(jobType string) error {
	if jobType == NODE_REG_JOB_TYPE_REGISTER || jobType == NODE_REG_JOB_TYPE_UPDATE {
		return nil
	}
	return rmerror.NewRMError(errors.New("invalid job type"), "invalid job type "+jobType)
}

// CreateNodeRegRequest creates a new node registration request entry
func (db *NodeRegRequestsDB) CreateNodeRegRequest(entry NodeRegRequestsEntry) error {
	if err := db.Ctx.IsAuthorized(utils.NodeAdminAdd, "*"); err != nil {
		return err
	}

	if err := IsValidNodeRegStatus(entry.Status); err != nil {
		return err
	}

	// JobType is optional on the wire for backward compat with code paths that
	// were written before the field existed; on read those rows are treated as
	// "register" via JobTypeOrDefault. If a caller does set it, it must be valid.
	if entry.JobType != "" {
		if err := IsValidJobType(entry.JobType); err != nil {
			return err
		}
	}

	timeNow := time.Now()
	entry.LastUpdatedAt = timeNow.Unix()
	entry.CreatedAt = timeNow.Unix()
	entry.GSIPK = nodeRegRequestsGSIPK

	if err := db.requests.Create(db.Ctx.Context, entry); err != nil {
		return rmerror.NewRMError(err, "failed to create node registration request")
	}

	return nil
}

// UpdateNodeRegRequest updates an existing node registration request
func (db *NodeRegRequestsDB) UpdateNodeRegRequest(entry NodeRegRequestsEntry) error {
	// RBAC check is done in at DB level: Only the user who created the request can update it

	entry.LastUpdatedAt = time.Now().Unix()
	entry.UserID = db.Ctx.GetID()

	if entry.Status != "" {
		if err := IsValidNodeRegStatus(entry.Status); err != nil {
			return err
		}
	}

	ownedByCaller := expression.Name(nodeRegRequestsUserIDCol).Equal(expression.Value(entry.UserID))
	if _, err := db.requests.UpdateFrom(db.Ctx.Context, entry, ddbu.WriteOpts{If: ownedByCaller}); err != nil {
		return rmerror.NewRMError(err, "failed to update node registration request")
	}

	return nil
}

// GetNodeRegRequest gets a node registration request entry
func (db *NodeRegRequestsDB) GetNodeRegRequest(requestId string) (*NodeRegRequestsEntry, error) {
	if err := db.Ctx.IsAuthorized(utils.NodeAdminRegisterStatus, "*"); err != nil {
		return nil, err
	}

	// A missing request returns an empty entry, not nil: jobs_api.LoadJob tests RequestID == "".
	entry, _, err := db.requests.Get(db.Ctx.Context, db.requests.Key(requestId))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get node registration request: "+requestId)
	}
	return &entry, nil
}

// ListNodeRegRequestsOutput holds the list result with pagination
type ListNodeRegRequestsOutput struct {
	Entries []NodeRegRequestsEntry
	NextKey string // Opaque token for pagination (base64-encoded last evaluated key)
}

// ListNodeRegRequests lists bulk-job requests ordered by created_at descending, optionally filtered by status and job_type. Empty jobTypeFilter returns every job. FILTER register matches job_type=register, missing job_type, or empty job_type (legacy rows). FILTER update matches job_type=update only.
func (db *NodeRegRequestsDB) ListNodeRegRequests(limit int64, startKey string, statusFilter string, jobTypeFilter string) (*ListNodeRegRequestsOutput, error) {
	if err := db.Ctx.IsAuthorized(utils.NodeAdminRegisterStatus, "*"); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 20
	}

	opts := ddbu.QueryOpts{
		Index:      NodeRegReqsListIndex,
		Limit:      int(limit),
		Descending: true,
		StartToken: startKey,
	}
	if filter, ok := listJobsFilter(statusFilter, jobTypeFilter); ok {
		opts.Filter = filter
	}

	page, err := db.requests.Query(db.Ctx.Context, expression.Key(nodeRegRequestsGSIHashKey).Equal(expression.Value(nodeRegRequestsGSIPK)), opts)
	if errors.Is(err, ddbu.ErrInvalidToken) {
		return nil, rmerror.NewRMError(err, "invalid start_key")
	}
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to list node registration requests")
	}
	return &ListNodeRegRequestsOutput{Entries: page.Items, NextKey: page.NextToken}, nil
}

func listJobsFilter(statusFilter, jobTypeFilter string) (expression.ConditionBuilder, bool) {
	var filter expression.ConditionBuilder
	hasFilter := false
	if statusFilter != "" {
		filter = expression.Name("status").Equal(expression.Value(statusFilter))
		hasFilter = true
	}
	if jobTypeCond, ok := jobTypeFilterCondition(jobTypeFilter); ok {
		if hasFilter {
			filter = expression.And(filter, jobTypeCond)
		} else {
			filter = jobTypeCond
			hasFilter = true
		}
	}
	return filter, hasFilter
}

func jobTypeFilterCondition(jobTypeFilter string) (expression.ConditionBuilder, bool) {
	switch jobTypeFilter {
	case NODE_REG_JOB_TYPE_UPDATE:
		return expression.Name("job_type").Equal(expression.Value(NODE_REG_JOB_TYPE_UPDATE)), true
	case NODE_REG_JOB_TYPE_REGISTER:
		return expression.Or(
			expression.Name("job_type").Equal(expression.Value(NODE_REG_JOB_TYPE_REGISTER)),
			expression.AttributeNotExists(expression.Name("job_type")),
			expression.Name("job_type").Equal(expression.Value("")),
		), true
	default:
		return expression.ConditionBuilder{}, false
	}
}
