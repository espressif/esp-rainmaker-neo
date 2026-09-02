// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package bulk_container

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/convert"
	"github.com/espressif/esp-rainmaker-neo/src/utils/parallel"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/s3util"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_failed_nodes_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/file"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// ContainerConfig holds all configuration needed for bulk container processing.
// JobType selects between the two flows the container can run: registration of
// new nodes or metadata update of existing ones. Empty defaults to "register"
// for backward compatibility with task definitions that don't yet set it.
type ContainerConfig struct {
	JobType         string
	CertFileS3Path  string
	UserID          string
	AdminGroupNames []string
	Tags            []string
	Capabilities    []string
	RequestID       string
}

// NewContainerConfigFromEnv creates a ContainerConfig from environment variables
func NewContainerConfigFromEnv() (*ContainerConfig, error) {
	certFileS3Path := os.Getenv("CERT_FILE_S3_PATH")
	if certFileS3Path == "" {
		return nil, rmerror.NewRMError(nil, "CERT_FILE_S3_PATH environment variable not set")
	}

	userID := os.Getenv("USER_ID")
	if userID == "" {
		return nil, rmerror.NewRMError(nil, "USER_ID environment variable not set")
	}

	requestID := os.Getenv("REQUEST_ID")
	if requestID == "" {
		return nil, rmerror.NewRMError(nil, "REQUEST_ID environment variable not set")
	}

	jobType := node_reg_req_db.JobTypeOrDefault(os.Getenv("JOB_TYPE"))
	if err := node_reg_req_db.IsValidJobType(jobType); err != nil {
		return nil, err
	}

	adminGroupNamesStr := os.Getenv("ADMIN_GROUP_NAMES")
	var adminGroupNames []string
	if adminGroupNamesStr != "" {
		adminGroupNames = strings.Split(adminGroupNamesStr, ",")
	}

	tagsStr := os.Getenv("TAGS")
	var tags []string
	if tagsStr != "" {
		tags = strings.Split(tagsStr, ",")
	}

	capabilitiesStr := os.Getenv("CAPABILITIES")
	var capabilities []string
	if capabilitiesStr != "" {
		capabilities = strings.Split(capabilitiesStr, ",")
	}

	return &ContainerConfig{
		JobType:         jobType,
		CertFileS3Path:  certFileS3Path,
		UserID:          userID,
		AdminGroupNames: adminGroupNames,
		Tags:            tags,
		Capabilities:    capabilities,
		RequestID:       requestID,
	}, nil
}

var (
	successCount int
	failureCount int
	failedNodes  []node_reg_failed_nodes_db.NodeRegFailedNodeEntry
	countMutex   sync.Mutex
)
var rmngCtx *rmngctx.RmngContext

// Headers of the csv file
const (
	NODE_ID_HEADER           = "node_id"
	CERT_HEADER              = "certs"
	ADMIN_GROUP_NAMES_HEADER = "admin_groups"
)

var (
	commonTags            []string
	commonAdminGroupNames []string
	commonCapabilities    []string
	adminId               string
	jobType               string
)

// rowGroupsAndTags merges the request-level common groups/tags with per-row
// CSV columns. The "extra" CSV columns (anything other than the reserved
// headers) become tags as colName:value, matching the registration flow.
func rowGroupsAndTags(req map[string]string) (groups, tags []string) {
	tags = append(tags, commonTags...)
	for extraHeader := range req {
		if extraHeader == NODE_ID_HEADER || extraHeader == CERT_HEADER || extraHeader == ADMIN_GROUP_NAMES_HEADER {
			continue
		}
		tags = append(tags, extraHeader+":"+req[extraHeader])
	}
	groups = append(groups, commonAdminGroupNames...)
	if csvGroups := req[ADMIN_GROUP_NAMES_HEADER]; csvGroups != "" {
		for _, g := range strings.Split(csvGroups, ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				groups = append(groups, g)
			}
		}
	}
	return groups, tags
}

// recordRowOutcome captures success or failure for a single CSV row under the
// shared mutex. Callers pass the operator-visible nodeID (cert CN when
// available, else the CSV node_id column) and the error from the per-row
// handler. The failure row is stamped with a coarse FailureCode so the
// dashboard can filter without parsing reason text, and the reason itself is
// the full wrapped error chain (not just the top wrapper) so the root cause
// survives to DDB.
func recordRowOutcome(reportedID string, err error) {
	countMutex.Lock()
	defer countMutex.Unlock()
	if err == nil {
		successCount++
		return
	}
	failureCount++
	// Mapped here, not in ClassifyFailure, to keep the db package free of a
	// node-package import.
	code := node_reg_failed_nodes_db.ClassifyFailure(err)
	if errors.Is(err, node.ErrNodeAlreadyRegistered) {
		code = node_reg_failed_nodes_db.FailureCodeDuplicateNodeID
	}
	failedNodes = append(failedNodes, node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
		NodeID: reportedID,
		Code:   string(code),
		Reason: rmerror.FormatErrorChain(err),
	})
}

// ProcessNodeRow is the per-row entry point passed to ProcessParallel. It
// dispatches on the package-level jobType (set in HandleContainer) so the
// same goroutine pool can drive either flow.
func ProcessNodeRow(req map[string]string) error {
	switch jobType {
	case node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE:
		return processNodeUpdate(req)
	default:
		// Empty / register / anything else falls back to the original behavior.
		return processNodeRegister(req)
	}
}

func processNodeRegister(req map[string]string) error {
	groups, tags := rowGroupsAndTags(req)

	nodeId, err := node.RegisterNodeInRmng(rmngCtx, req[CERT_HEADER], "", groups, tags, adminId, commonCapabilities)
	if err != nil {
		// Cert parsing fails before the cert CN can be extracted, so fall back
		// to the operator-supplied node_id from the CSV for failure reporting.
		reportedID := nodeId
		if reportedID == "" {
			reportedID = req[NODE_ID_HEADER]
		}
		rlog.Error(rmngCtx).Err(err).Msg("failed to register node: " + reportedID)
		recordRowOutcome(reportedID, err)
		return err
	}
	recordRowOutcome(nodeId, nil)
	return nil
}

func processNodeUpdate(req map[string]string) error {
	nodeID := req[NODE_ID_HEADER]
	if nodeID == "" {
		err := rmerror.NewRMError(nil, "row missing node_id column")
		recordRowOutcome("", err)
		return err
	}
	groups, tags := rowGroupsAndTags(req)
	// Cert column is optional in update mode: empty string means leave the
	// cert binding alone, non-empty triggers a cloud-side cert replacement
	// (correcting a mistakenly-registered PEM).
	newCert := req[CERT_HEADER]

	err := node.UpdateNodeInRmng(rmngCtx, nodeID, newCert, groups, tags, nil)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("failed to update node: " + nodeID)
		recordRowOutcome(nodeID, err)
		return err
	}
	recordRowOutcome(nodeID, nil)
	return nil
}

// readNodesFromS3 returns both the parsed rows and the CSV header in its
// original column order. The header is needed to reproduce the input layout
// when writing the filtered failed-rows CSV (§3.5.5) — the row maps don't
// retain column order.
func readNodesFromS3(ctx context.Context, s3Path string) ([]string, []map[string]string, error) {
	bucket, key, err := s3util.GetBucketKey(s3Path)
	if err != nil {
		return nil, nil, rmerror.NewRMError(err, "failed to get bucket and key from S3 path")
	}

	f, err := file.NewFile(bucket, key)
	if err != nil {
		return nil, nil, rmerror.NewRMError(err, "failed to create file")
	}

	content, err := f.ReadContent(ctx)
	if err != nil {
		return nil, nil, rmerror.NewRMError(err, "failed to read file")
	}

	header, nodes, err := convert.ReadCSVToStructWithHeaders(content)
	if err != nil {
		return nil, nil, rmerror.NewRMError(err, "failed to parse CSV file")
	}

	return header, nodes, nil
}

// failedCertsKey derives the S3 key for the failed-rows CSV from the input
// CSV path: same bucket and prefix, filename
// "<requestId>_failed_node_certs.csv". Deterministic in the request_id, so
// each job owns a distinct object and there is no overwrite ambiguity.
func failedCertsKey(inputS3Path, requestID string) (bucket, key string, err error) {
	bucket, inputKey, err := s3util.GetBucketKey(inputS3Path)
	if err != nil {
		return "", "", rmerror.NewRMError(err, "failed to parse input CSV path")
	}
	prefix := ""
	if idx := strings.LastIndex(inputKey, "/"); idx >= 0 {
		prefix = inputKey[:idx+1]
	}
	return bucket, prefix + requestID + "_failed_node_certs.csv", nil
}

// writeFailedNodesCSV builds a filtered copy of the original input CSV — the
// header in its original column order followed by every row whose node_id
// column matches a recorded failure — and writes it to S3. Returns the full
// s3:// path of the written object. Matching is on the operator-supplied
// node_id column, the same identifier recorded as the DB failure key.
func writeFailedNodesCSV(ctx context.Context, inputS3Path, requestID string, header []string, nodes []map[string]string, failures []node_reg_failed_nodes_db.NodeRegFailedNodeEntry) (string, error) {
	bucket, key, err := failedCertsKey(inputS3Path, requestID)
	if err != nil {
		return "", err
	}

	failedIDs := make(map[string]struct{}, len(failures))
	for _, f := range failures {
		failedIDs[f.NodeID] = struct{}{}
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(header); err != nil {
		return "", rmerror.NewRMError(err, "failed to write failed-nodes CSV header")
	}
	for _, row := range nodes {
		if _, ok := failedIDs[row[NODE_ID_HEADER]]; !ok {
			continue
		}
		record := make([]string, len(header))
		for i, col := range header {
			record[i] = row[col]
		}
		if err := w.Write(record); err != nil {
			return "", rmerror.NewRMError(err, "failed to write failed-nodes CSV row")
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", rmerror.NewRMError(err, "failed to assemble failed-nodes CSV")
	}

	if err := s3util.PutObject(ctx, bucket, key, bytes.NewReader(buf.Bytes())); err != nil {
		return "", err
	}
	return s3util.GetS3Path(bucket, key), nil
}

func HandleContainer(config *ContainerConfig) error {
	resolvedJobType := node_reg_req_db.JobTypeOrDefault(config.JobType)
	noun := jobNoun(resolvedJobType)

	rlog.Info(rmngCtx).Str("job_type", resolvedJobType).Msg("Starting bulk node " + noun + " task")
	rlog.Debug(rmngCtx).Str("request_id", config.RequestID).Msg("Bulk node " + noun + " request id")

	// Reset counters and failure detail for each container run
	countMutex.Lock()
	successCount = 0
	failureCount = 0
	failedNodes = nil
	countMutex.Unlock()

	adminId = config.UserID
	commonTags = config.Tags
	commonAdminGroupNames = config.AdminGroupNames
	commonCapabilities = config.Capabilities
	jobType = resolvedJobType

	user := user.NewUser(adminId)
	rmngCtx = rmngctx.NewRmngContext(user)

	// TODO: Once we figure out rbac for admins, remove this
	rmngCtx.SetAllow(utils.NodeAdminAll, "*")
	rmngCtx.SetAllow(utils.NodeWriteShadow, "*")
	if resolvedJobType == node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE {
		// Update flow needs to read node_details for the existence check.
		rmngCtx.SetAllow(utils.NodeAll, "*")
	}

	dbClient := node_reg_req_db.NewNodeRegRequestsDB(rmngCtx)

	// Update status to started (record was created by the Lambda handler)
	err := dbClient.UpdateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
		RequestID: config.RequestID,
		Message:   "Bulk node " + noun + " started",
		Status:    node_reg_req_db.NODE_REG_STATUS_STARTED,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to update node reg request to started")
	}

	header, nodes, err := readNodesFromS3(rmngCtx.Context, config.CertFileS3Path)
	if err != nil {
		return rmerror.NewRMError(err, "failed to read nodes from S3 CSV")
	}
	if len(nodes) == 0 {
		return rmerror.NewRMError(nil, "no nodes found in CSV file")
	}

	// Mark request as data loaded
	err = dbClient.UpdateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
		RequestID:  config.RequestID,
		Status:     node_reg_req_db.NODE_REG_STATUS_DATA_LOADED,
		TotalCount: len(nodes),
		Message:    "Bulk node " + noun + " input file loaded",
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to update node reg request")
	}

	_, _, err = parallel.ProcessParallel(rmngCtx, nodes, ProcessNodeRow, parallel.ParallelOptions{
		CollectResults: false,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to process nodes")
	}

	// Clone before persisting so the slice passed to RecordFailures is
	// independent of the package-level failedNodes (which is reset on the
	// next HandleContainer call). ProcessParallel's wg.Wait() already
	// guarantees all worker writes are visible — no lock needed here.
	failuresSnapshot := slices.Clone(failedNodes)

	completionMessage := "Bulk node " + noun + " completed"
	var failedFileS3Path string
	if len(failuresSnapshot) > 0 {
		failuresDB := node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(rmngCtx)
		if recErr := failuresDB.RecordFailures(config.RequestID, failuresSnapshot); recErr != nil {
			// Failure-detail loss is non-fatal: counts are still accurate, the
			// per-node logs in CloudWatch remain authoritative, and the operator
			// sees a flagged message on the job's status.
			rlog.Error(rmngCtx).Err(recErr).Msg("failed to record failed-nodes detail")
			completionMessage = "Bulk node " + noun + " completed; failed-nodes detail unavailable, see container logs"
		}

		// Write the re-uploadable, cert-bearing CSV of failed rows to S3
		// (§3.5.5). The write is non-fatal on the same terms as the DDB
		// detail above: a miss leaves failed_file_s3_path unset and flags
		// the status message; the DDB audit list stays authoritative.
		path, csvErr := writeFailedNodesCSV(rmngCtx.Context, config.CertFileS3Path, config.RequestID, header, nodes, failuresSnapshot)
		if csvErr != nil {
			rlog.Error(rmngCtx).Err(csvErr).Msg("failed to write failed-nodes CSV to S3")
			completionMessage = "Bulk node " + noun + " completed; failed-nodes CSV unavailable, see container logs"
		} else {
			failedFileS3Path = path
		}
	}

	// Mark request as completed
	err = dbClient.UpdateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
		RequestID:        config.RequestID,
		Status:           node_reg_req_db.NODE_REG_STATUS_COMPLETED,
		SuccessCount:     &successCount,
		FailedCount:      &failureCount,
		FailedFileS3Path: failedFileS3Path,
		Message:          completionMessage,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to update node reg request")
	}

	rlog.Info(rmngCtx).Int("success_count", successCount).Int("failure_count", failureCount).Msg("Bulk node " + noun + " task completed successfully.")

	return nil
}

// jobNoun gives a job-type-aware verb for log lines and status messages.
func jobNoun(jt string) string {
	if jt == node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE {
		return "update"
	}
	return "registration"
}
