// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group

import (
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/collections"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// MatterCapabilityName is the identifier for the Matter fabric capability.
// It is also used as the node-level "matter" capability tag on the group-node row.
const MatterCapabilityName = "matter"

// NodeCapabilityRMNG is the node-level capability tag for a RainMaker node.
const NodeCapabilityRMNG = "rmng"

// MatterCapability implements GroupCapability for Matter fabric support
type MatterCapability struct{}

// init registers the Matter capability.
func init() {
	RegisterCapability(&MatterCapability{})
}

// Name returns the unique identifier for this capability
func (m *MatterCapability) Name() string {
	return MatterCapabilityName
}

// OnGroupCreate is called when a group with Matter capability is created.
// It generates all the Matter fabric data: Fabric ID (derived from group ID), Root CA, IPK, and CAT IDs.
func (m *MatterCapability) OnGroupCreate(ctx *rmngctx.RmngContext, group *Group) (*CapabilityData, error) {
	matterGroup, err := NewMatterGroupFromScratch(group)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to create matter group")
	}

	// Convert struct to map using JSON tags for consistent key names
	dbFields := make(map[string]interface{})
	if err := utils.ConvertAnyToAny(matterGroup.MatterData, &dbFields); err != nil {
		return nil, rmerror.NewRMError(err, "failed to convert matter data to map")
	}

	return &CapabilityData{
		DBFields: dbFields,
	}, nil
}

// OnGroupDelete is called when a group with Matter capability is deleted.
// Currently no special cleanup is needed as the group deletion handles DB record removal.
func (m *MatterCapability) OnGroupDelete(ctx *rmngctx.RmngContext, groupID string) error {
	// No special cleanup needed for Matter fabric
	// The group deletion will handle removing the DB records
	return nil
}

// OnUserJoinGroup does not need to provision per-user state. Matter controller
// Node IDs are derived when a user requests a NOC.
func (m *MatterCapability) OnUserJoinGroup(ctx *rmngctx.RmngContext, groupID string, userID string) error {
	return nil
}

// OnUserExitGroup is called when a user exits/leaves a group with Matter capability.
// It increments the appropriate CAT ID version based on the user's access type.
// This ensures users who remain in the group get updated NOCs with new CAT ID versions.
func (m *MatterCapability) OnUserExitGroup(ctx *rmngctx.RmngContext, groupID string, userID string, accessType string) error {
	groupDB := group_db.NewGroupDB(ctx)

	// Get current capability data
	capData, err := groupDB.GetCapabilityData(groupID, MatterCapabilityName)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get matter capability data")
	}

	if capData == nil {
		return rmerror.NewRMError(nil, "no matter capability data found")
	}

	// Determine which CAT ID to update based on access type
	var catIDKey string
	if accessType == string(utils.GroupPrimaryAccess) {
		catIDKey = KeyGroupCATIDAdmin
	} else {
		catIDKey = KeyGroupCATIDOperate
	}

	// Get current CAT ID
	currentCATID, ok := capData[catIDKey].(string)
	if !ok || currentCATID == "" {
		return rmerror.NewRMError(nil, "CAT ID not found in capability data")
	}

	// Increment CAT ID version
	newCATID, err := IncrementCATIDVersion(currentCATID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to increment CAT ID version")
	}

	// Update capability data with new CAT ID
	capData[catIDKey] = newCATID

	// Write back to database
	err = groupDB.UpdateCapabilityData(groupID, MatterCapabilityName, capData)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update capability data")
	}

	return nil
}

// GetResponseData returns the Matter-specific data to include in API responses.
// It retrieves stored data from the database.
func (m *MatterCapability) GetResponseData(ctx *rmngctx.RmngContext, group *Group) (map[string]interface{}, error) {
	capData, ok := group.CapabilityData[MatterCapabilityName]
	if !ok || capData == nil {
		return nil, rmerror.NewRMError(nil, "no matter capability data found for group")
	}

	return map[string]interface{}{
		KeyFabricID:          capData[KeyFabricID],
		KeyRootCA:            capData[KeyRootCA],
		KeyIPK:               capData[KeyIPK],
		KeyGroupCATIDAdmin:   capData[KeyGroupCATIDAdmin],
		KeyGroupCATIDOperate: capData[KeyGroupCATIDOperate],
	}, nil
}

// MatterCapabilityData holds the Matter-specific data stored in the cap_matter JSON column
type MatterCapabilityData struct {
	FabricID          string `json:"fabric_id"`
	RootCA            string `json:"root_ca"`
	RootCAPrivateKey  string `json:"root_ca_priv_key"`
	IPK               string `json:"ipk"`
	GroupCATIDAdmin   string `json:"group_cat_id_admin"`
	GroupCATIDOperate string `json:"group_cat_id_operate"`
}

// MatterResponseData is the structure returned in API responses
type MatterResponseData struct {
	FabricID          string `json:"fabric_id"`
	RootCA            string `json:"root_ca"`
	IPK               string `json:"ipk"`
	GroupCATIDAdmin   string `json:"group_cat_id_admin"`
	GroupCATIDOperate string `json:"group_cat_id_operate"`
}

// MatterGroup represents a group with Matter fabric capability.
// It embeds the base Group and adds Matter-specific data and methods.
type MatterGroup struct {
	*Group
	MatterData *MatterCapabilityData
}

// FabricIDFromGroupID creates a Matter Fabric ID from a group ID using ASCII-to-hex encoding.
// The group ID is converted to its hex representation and padded/truncated to 16 hex chars (8 bytes).
// Example: "abc123" -> hex("abc123") = "616263313233" -> pad to "6162633132330000"
func FabricIDFromGroupID(groupID string) string {
	hexStr := hex.EncodeToString([]byte(groupID))
	// Pad with zeros if shorter than 16 chars, or truncate if longer
	if len(hexStr) < FabricIDHexLength {
		hexStr = hexStr + strings.Repeat("0", FabricIDHexLength-len(hexStr))
	} else if len(hexStr) > FabricIDHexLength {
		hexStr = hexStr[:FabricIDHexLength]
	}
	return strings.ToUpper(hexStr)
}

// GroupIDFromFabricID extracts the group ID from a Matter Fabric ID.
// The fabric ID is decoded from hex back to ASCII, with trailing null bytes removed.
// Example: "6162633132330000" -> "abc123"
func GroupIDFromFabricID(fabricID string) (string, error) {
	// Remove any leading/trailing whitespace and convert to lowercase for decoding
	fabricID = strings.TrimSpace(strings.ToLower(fabricID))
	if len(fabricID) != FabricIDHexLength {
		return "", fmt.Errorf("invalid fabric ID length: expected %d, got %d", FabricIDHexLength, len(fabricID))
	}

	decoded, err := hex.DecodeString(fabricID)
	if err != nil {
		return "", fmt.Errorf("failed to decode fabric ID: %w", err)
	}

	// Remove trailing null bytes
	result := strings.TrimRight(string(decoded), "\x00")
	return result, nil
}

// NewMatterGroup creates a MatterGroup from an existing Group.
// The Group must have been loaded with capability data (via LoadGroup or GetGroupByID).
// Returns an error if the group doesn't have Matter capability data.
func NewMatterGroup(group *Group) (*MatterGroup, error) {
	// Check if capability data is already loaded in the Group
	capData, ok := group.CapabilityData[MatterCapabilityName]
	if !ok || capData == nil {
		return nil, rmerror.NewRMError(nil, "group does not have matter capability data")
	}

	// Parse the capability data using JSON tags for consistent key names
	matterData := &MatterCapabilityData{}
	if err := utils.ConvertAnyToAny(capData, matterData); err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse matter capability data")
	}

	return &MatterGroup{
		Group:      group,
		MatterData: matterData,
	}, nil
}

// NewMatterGroupFromScratch creates a new MatterGroup with fresh Matter fabric data.
// This is used when creating a new group with Matter capability.
// The groupID is used to deterministically generate the Fabric ID.
func NewMatterGroupFromScratch(group *Group) (*MatterGroup, error) {
	// Generate Fabric ID from group ID
	fabricID := FabricIDFromGroupID(group.GroupID)

	// Generate Root CA certificate and private key
	rootCA, err := CreateRootCACertificate(fabricID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to generate root CA certificate")
	}

	// Generate IPK
	ipk, err := GenerateIPK()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to generate IPK")
	}

	// Generate CAT IDs
	catIDAdmin, err := GenerateCATID(true)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to generate admin CAT ID")
	}
	catIDOperate, err := GenerateCATID(false)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to generate operate CAT ID")
	}

	matterData := &MatterCapabilityData{
		FabricID:          fabricID,
		RootCA:            rootCA.CertificatePEM,
		RootCAPrivateKey:  rootCA.PrivateKeyPEM,
		IPK:               ipk,
		GroupCATIDAdmin:   catIDAdmin,
		GroupCATIDOperate: catIDOperate,
	}

	return &MatterGroup{
		Group:      group,
		MatterData: matterData,
	}, nil
}

// GetFabricID returns the Fabric ID for this Matter group.
// This can also be computed from the group ID using FabricIDFromGroupID.
func (mg *MatterGroup) GetFabricID() string {
	if mg.MatterData != nil {
		return mg.MatterData.FabricID
	}
	return FabricIDFromGroupID(mg.GroupID)
}

// IsMatterNodeIDFormat reports whether s has the format of a generated pure-Matter node
// id: a 16-character hex string. Used to classify legacy/untagged nodes in get-groups.
func IsMatterNodeIDFormat(s string) bool {
	if len(s) != FabricIDHexLength {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// ContainsMatterCapability checks if the capabilities list includes the Matter capability
func ContainsMatterCapability(capabilities []string) bool {
	for _, cap := range capabilities {
		if cap == MatterCapabilityName {
			return true
		}
	}
	return false
}

// IsPureMatterNode reports whether the capability tags are "matter" but not "rmng".
func IsPureMatterNode(capabilities []string) bool {
	if !ContainsMatterCapability(capabilities) {
		return false
	}
	isRMNG, _ := collections.ItemExists(capabilities, NodeCapabilityRMNG)
	return !isRMNG
}

// MatterNodeIDFromThingName creates a Matter Node ID from a thing name (node ID/UUID).
// If the thingName is already a valid 16-char hex string (representing a 64-bit unsigned integer),
// it is used directly (uppercased). Otherwise, ASCII-to-hex encoding is used as a fallback,
// where the thing name is converted to its hex representation and padded/truncated to 16 hex chars (8 bytes).
// Example (hex passthrough): "A1B2C3D4E5F60718" -> "A1B2C3D4E5F60718"
// Example (ASCII fallback):  "node123" -> hex("node123") = "6e6f6465313233" -> pad to "6E6F646531323300"
func MatterNodeIDFromThingName(thingName string) string {
	// If the thingName is exactly 16 hex chars, it can be directly used as a Matter Node ID

	// If the thing name is not hex it will fallback to the ASCII fallback
	if len(thingName) == FabricIDHexLength {
		if _, err := hex.DecodeString(thingName); err == nil {
			return strings.ToUpper(thingName)
		}
	}

	// Fallback: ASCII-to-hex encoding
	hexStr := hex.EncodeToString([]byte(thingName))
	// Pad with zeros if shorter than 16 chars, or truncate if longer
	if len(hexStr) < FabricIDHexLength {
		hexStr = hexStr + strings.Repeat("0", FabricIDHexLength-len(hexStr))
	} else if len(hexStr) > FabricIDHexLength {
		hexStr = hexStr[:FabricIDHexLength]
	}
	return strings.ToUpper(hexStr)
}

// LoadMatterGroupFromGrpID loads a group by ID and returns a MatterGroup.
// It verifies the user has access to the group and that the group has Matter capability.
// Returns an error if the group doesn't exist, user doesn't have access, or group lacks Matter capability.
func LoadMatterGroupFromGrpID(ctx *rmngctx.RmngContext, groupID string) (*MatterGroup, error) {
	// Load the group with capability data
	userGroups, err := ListGroupForUser(ctx, groupID, false)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to load group")
	}

	if len(userGroups) == 0 {
		return nil, rmerror.NewRMError(nil, "group not found")
	}

	loadedGroup := userGroups[0]

	// Verify the group has Matter capability
	if !ContainsMatterCapability(loadedGroup.Capabilities) {
		return nil, rmerror.NewRMError(ErrNotMatterCapable, "group does not have Matter capability")
	}

	// Create MatterGroup wrapper
	return NewMatterGroup(&loadedGroup)
}

// NOCResult holds the result of NOC generation
type NOCResult struct {
	NOC          string
	MatterNodeID string
}

// GetNOC generates a Node Operational Certificate for the current user's controller.
// The controller Node ID is derived from the authenticated user, fabric, and CSR key.
func (mg *MatterGroup) GetNOC(ctx *rmngctx.RmngContext, csrPEM string) (*NOCResult, error) {
	userGroupDB := user_group_db.NewUserGroupDB(ctx)

	// Get the user's group entry to verify membership and determine access level.
	userGroup, err := userGroupDB.GetUserGroup(mg.GroupID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get user group access")
	}

	rmngUserID := ctx.Accessor.GetID()
	if rmngUserID == "" {
		return nil, rmerror.NewRMError(nil, "rmng user ID not found")
	}

	csr, err := ParseCSR(csrPEM)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse CSR")
	}
	publicKey, err := ECDSAP256PublicKey(csr.PublicKey)
	if err != nil {
		return nil, rmerror.NewRMError(err, "invalid CSR public key")
	}
	matterNodeID, err := DeriveMatterUserNodeID(mg.MatterData.FabricID, rmngUserID, publicKey)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to derive Matter controller Node ID")
	}
	isAdmin := userGroup.AccessType == utils.GroupPrimaryAccess

	// Generate the NOC
	noc, err := mg.generateNOC(csr, matterNodeID, isAdmin)
	if err != nil {
		return nil, err
	}

	return &NOCResult{
		NOC:          noc,
		MatterNodeID: matterNodeID,
	}, nil
}

// buildNOC parses the group's Root CA material and issues a NOC signed by
// the group's Root CA for the given Matter node ID. A non-empty groupCATID embeds a CASE
// Authenticated Tag (used for user NOCs); pass "" for device NOCs, which carry no CAT ID.
func (mg *MatterGroup) buildNOC(csr *x509.CertificateRequest, matterNodeID string, groupCATID string) (string, error) {
	// Parse the Root CA certificate
	rootCACert, err := ParseCertificatePEM(mg.MatterData.RootCA)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to parse Root CA certificate")
	}

	// Parse the Root CA private key
	rootCAPrivKey, err := ParsePrivateKeyPEM(mg.MatterData.RootCAPrivateKey)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to parse Root CA private key")
	}

	// Generate the NOC
	noc, err := CreateNOC(&NOCInput{
		CSR:              csr,
		FabricID:         mg.MatterData.FabricID,
		MatterNodeID:     matterNodeID,
		GroupCATID:       groupCATID,
		RootCACert:       rootCACert,
		RootCAPrivateKey: rootCAPrivKey,
	})
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to create NOC")
	}

	return noc, nil
}

// generateNOC generates a Node Operational Certificate for a user in a Matter-enabled group.
// The CSR must be provided by the user/client, and the NOC is signed by the group's Root CA.
// isAdmin determines whether to use the admin or operate CAT ID.
func (mg *MatterGroup) generateNOC(csr *x509.CertificateRequest, matterNodeID string, isAdmin bool) (string, error) {
	groupCATID := mg.MatterData.GroupCATIDOperate
	if isAdmin {
		groupCATID = mg.MatterData.GroupCATIDAdmin
	}
	return mg.buildNOC(csr, matterNodeID, groupCATID)
}

// DeviceNOCResult holds the result of Device NOC generation
type DeviceNOCResult struct {
	NOC          string
	MatterNodeID string
}

// GenerateDeviceNOC generates a Node Operational Certificate for a device.
// The CSR is provided by the device, and the NOC is signed by the group's Root CA.
// Devices get NOCs without CAT IDs (unlike users who get admin/operate CAT IDs).
func (mg *MatterGroup) GenerateDeviceNOC(csrPEM string, thingName string) (*DeviceNOCResult, error) {
	// Generate matter_node_id from thing name
	matterNodeID := MatterNodeIDFromThingName(thingName)

	csr, err := ParseCSR(csrPEM)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse CSR")
	}

	// Devices get NOCs without CAT IDs (unlike users who get admin/operate CAT IDs).
	noc, err := mg.buildNOC(csr, matterNodeID, "")
	if err != nil {
		return nil, err
	}

	return &DeviceNOCResult{
		NOC:          noc,
		MatterNodeID: matterNodeID,
	}, nil
}
