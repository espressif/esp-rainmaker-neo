// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"sync"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iot"
	"github.com/aws/aws-sdk-go-v2/service/iot/types"
	"github.com/aws/smithy-go"
)

type Certificate struct {
	Arn    string
	Status types.CertificateStatus
	Cert   string
}

type Things struct {
	Name           string
	CertificateIds []string
	// Track group memberships
	Groups []string
	// Track thing attributes (e.g. "group_id")
	Attributes map[string]string
}

type IoTClientMock struct {
	// Key is the thing name
	Things map[string]Things
	// Key is the certificate ID
	Certificates map[string]Certificate
	// Track thing group memberships - Key is group name, value is list of thing names
	ThingGroups map[string][]string
	// Track thing group parent relationships - Key is child group name, value is parent group name
	ThingGroupParents map[string]string
	// Track policy attachments - Key is target (cert ARN), value is list of policy names
	AttachedPolicies map[string][]string
	// Track topic-rule payloads for the GetTopicRule / ReplaceTopicRule mock — Key is rule name
	TopicRules map[string]*types.TopicRulePayload

	// Mutex to protect concurrent access to maps
	mutex sync.RWMutex

	// Error forcing flags
	ForceThingCreationError    bool
	ForcePolicyAttachmentError bool
	ForceGroupAdditionError    bool
	ForceGroupCreationError    bool
	ForceCertificateError      bool
	ForceCertAttachmentError   bool
}

func NewIoTClientMock() *IoTClientMock {
	return &IoTClientMock{
		Things:            make(map[string]Things),
		Certificates:      make(map[string]Certificate),
		ThingGroups:       make(map[string][]string),
		ThingGroupParents: make(map[string]string),
	}
}

func (m *IoTClientMock) Print() {
	fmt.Println("Things:", m.Things)
	fmt.Println("Certificates:", m.Certificates)
	fmt.Println("ThingGroups:", m.ThingGroups)
}

// Direct access and verification functions for testing

// GetThingDirect retrieves a thing directly from the mock
func (m *IoTClientMock) GetThingDirect(thingName string) (*Things, bool) {
	thing, exists := m.Things[thingName]
	if !exists {
		return nil, false
	}
	return &thing, true
}

// GetThingsDirect returns all things in the mock
func (m *IoTClientMock) GetThingsDirect() map[string]Things {
	return m.Things
}

// GetCertificateDirect retrieves a certificate by ID directly from the mock
func (m *IoTClientMock) GetCertificateDirect(certID string) (*Certificate, bool) {
	cert, exists := m.Certificates[certID]
	if !exists {
		return nil, false
	}
	return &cert, true
}

// GetCertificatesDirect returns all certificates in the mock
func (m *IoTClientMock) GetCertificatesDirect() map[string]Certificate {
	return m.Certificates
}

// FindCertificateByPEMDirect finds a certificate by its PEM content
func (m *IoTClientMock) FindCertificateByPEMDirect(certPEM string) (*Certificate, string, bool) {
	for certID, cert := range m.Certificates {
		if cert.Cert == certPEM {
			certCopy := cert
			return &certCopy, certID, true
		}
	}
	return nil, "", false
}

// VerifyThingExists checks if a thing exists in the mock
func (m *IoTClientMock) VerifyThingExists(thingName string) bool {
	_, exists := m.Things[thingName]
	return exists
}

// VerifyCertificateExists checks if a certificate exists in the mock by PEM content
func (m *IoTClientMock) VerifyCertificateExists(certPEM string) bool {
	_, _, exists := m.FindCertificateByPEMDirect(certPEM)
	return exists
}

// VerifyCertificateActive checks if a certificate is active in the mock by PEM content
func (m *IoTClientMock) VerifyCertificateActive(certPEM string) bool {
	cert, _, exists := m.FindCertificateByPEMDirect(certPEM)
	if !exists {
		return false
	}
	return cert.Status == types.CertificateStatusActive
}

// VerifyThingHasCertificate checks if a thing has a certificate attached
func (m *IoTClientMock) VerifyThingHasCertificate(thingName string, certPEM string) bool {
	// Use ListThingPrincipals and DescribeCertificate, consistent with AWS SDK
	ctx := context.Background()

	// First, find certificate ID from PEM content
	_, certID, exists := m.FindCertificateByPEMDirect(certPEM)
	if !exists {
		return false
	}

	// Use ListThingPrincipals to get all certificates attached to thing
	principals, err := m.ListThingPrincipals(ctx, &iot.ListThingPrincipalsInput{
		ThingName: aws.String(thingName),
	})
	if err != nil {
		return false
	}

	// Check if the certificate ARN is in the principals
	for _, principal := range principals.Principals {
		certIDFromPrincipal, err := iotutil.ExtractCertIDFromARN(principal)
		if err != nil {
			return false
		}
		if certIDFromPrincipal == certID {
			return true
		}
	}

	return false
}

// GetThingGroupsDirect returns the groups that a thing belongs to
func (m *IoTClientMock) GetThingGroupsDirect(thingName string) []string {
	thing, exists := m.Things[thingName]
	if !exists {
		return nil
	}
	return thing.Groups
}

// VerifyThingInGroup checks if a thing is in a specific group
func (m *IoTClientMock) VerifyThingInGroup(thingName string, groupName string) bool {
	members, exists := m.ThingGroups[groupName]
	if !exists {
		return false
	}

	for _, member := range members {
		if member == thingName {
			return true
		}
	}
	return false
}

// VerifyThingInGroups checks if a thing is in all the specified groups
func (m *IoTClientMock) VerifyThingInGroups(thingName string, groupNames []string) bool {
	for _, group := range groupNames {
		if !m.VerifyThingInGroup(thingName, group) {
			return false
		}
	}
	return true
}

func (m *IoTClientMock) ListThingPrincipals(ctx context.Context, params *iot.ListThingPrincipalsInput, optFns ...func(*iot.Options)) (*iot.ListThingPrincipalsOutput, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	thing, exists := m.Things[*params.ThingName]
	if !exists {
		return &iot.ListThingPrincipalsOutput{Principals: []string{}}, nil
	}
	arns := make([]string, 0, len(thing.CertificateIds))
	for _, certID := range thing.CertificateIds {
		arns = append(arns, m.Certificates[certID].Arn)
	}
	return &iot.ListThingPrincipalsOutput{
		Principals: arns,
	}, nil
}

func (m *IoTClientMock) DescribeCertificate(ctx context.Context, params *iot.DescribeCertificateInput, optFns ...func(*iot.Options)) (*iot.DescribeCertificateOutput, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	certID := *params.CertificateId
	cert, exists := m.Certificates[certID]
	if !exists {
		return nil, rmerror.NewRMError(fmt.Errorf("certificate not found: %s", certID), "")
	}
	return &iot.DescribeCertificateOutput{
		CertificateDescription: &types.CertificateDescription{
			CertificateArn: aws.String(cert.Arn),
			CertificateId:  aws.String(certID),
			Status:         cert.Status,
			CertificatePem: aws.String(cert.Cert),
		},
	}, nil
}
func (m *IoTClientMock) RegisterCertificateWithoutCA(ctx context.Context, params *iot.RegisterCertificateWithoutCAInput, optFns ...func(*iot.Options)) (*iot.RegisterCertificateWithoutCAOutput, error) {

	if m.ForceCertificateError {
		return nil, &smithy.GenericAPIError{
			Code:    "InternalFailure",
			Message: "Forced certificate error",
		}
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check if this certificate PEM has already been registered
	for _, cert := range m.Certificates {
		if cert.Cert == *params.CertificatePem {
			return nil, &smithy.GenericAPIError{
				Code:    "ResourceAlreadyExistsException",
				Message: "Certificate already exists",
			}
		}
	}

	// Generate consistent certID and ARN
	certID, err := iotutil.GetCertIDFromPEM(*params.CertificatePem)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get certID from PEM")
	}
	arn := fmt.Sprintf("arn:aws:iot:us-west-2:123456789012:cert/%s", certID)

	m.Certificates[certID] = Certificate{
		Arn:    arn,
		Status: types.CertificateStatusActive,
		Cert:   *params.CertificatePem,
	}

	return &iot.RegisterCertificateWithoutCAOutput{
		CertificateArn: aws.String(arn),
		CertificateId:  aws.String(certID),
	}, nil
}

func (m *IoTClientMock) CreateThing(ctx context.Context, params *iot.CreateThingInput, optFns ...func(*iot.Options)) (*iot.CreateThingOutput, error) {

	if m.ForceThingCreationError {
		return nil, &smithy.GenericAPIError{
			Code:    "InternalFailure",
			Message: "Forced thing creation error",
		}
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	thingName := *params.ThingName
	if _, exists := m.Things[thingName]; exists {
		return nil, &smithy.GenericAPIError{
			Code:    "ResourceAlreadyExistsException",
			Message: "Thing already exists",
		}
	}

	attrs := make(map[string]string)
	if params.AttributePayload != nil {
		for k, v := range params.AttributePayload.Attributes {
			attrs[k] = v
		}
	}
	m.Things[thingName] = Things{
		Name:           thingName,
		CertificateIds: []string{},
		Groups:         []string{},
		Attributes:     attrs,
	}

	return &iot.CreateThingOutput{
		ThingArn:  aws.String(fmt.Sprintf("arn:aws:iot:us-west-2:123456789012:thing/%s", thingName)),
		ThingName: aws.String(thingName),
	}, nil
}

func (m *IoTClientMock) AttachThingPrincipal(ctx context.Context, params *iot.AttachThingPrincipalInput, optFns ...func(*iot.Options)) (*iot.AttachThingPrincipalOutput, error) {
	if m.ForceCertAttachmentError {
		return nil, &smithy.GenericAPIError{
			Code:    "InternalFailure",
			Message: "Forced certificate attachment error",
		}
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	thingName := *params.ThingName
	principal := *params.Principal

	thing, exists := m.Things[thingName]
	if !exists {
		return nil, rmerror.NewRMError(fmt.Errorf("thing not found: %s", thingName), "")
	}

	// Extract cert ID from ARN
	certID, err := iotutil.ExtractCertIDFromARN(principal)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to extract certificate ID from ARN")
	}

	// Make sure the certificate exists
	if _, certExists := m.Certificates[certID]; !certExists {
		return nil, rmerror.NewRMError(fmt.Errorf("certificate not found: %s", certID), "")
	}

	// Add the certID to the thing's certificates if not already there
	found := false
	for _, id := range thing.CertificateIds {
		if id == certID {
			found = true
			break
		}
	}

	if !found {
		thing.CertificateIds = append(thing.CertificateIds, certID)
		m.Things[thingName] = thing
	}

	return &iot.AttachThingPrincipalOutput{}, nil
}

func (m *IoTClientMock) ListThingGroups(ctx context.Context, params *iot.ListThingGroupsInput, optFns ...func(*iot.Options)) (*iot.ListThingGroupsOutput, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Return all the thing groups in our map
	groups := make([]types.GroupNameAndArn, 0, len(m.ThingGroups))
	for groupName := range m.ThingGroups {
		groups = append(groups, types.GroupNameAndArn{
			GroupName: aws.String(groupName),
			GroupArn:  aws.String(fmt.Sprintf("arn:aws:iot:us-west-2:123456789012:thinggroup/%s", groupName)),
		})
	}
	return &iot.ListThingGroupsOutput{
		ThingGroups: groups,
	}, nil
}

func (m *IoTClientMock) CreateThingGroup(ctx context.Context, params *iot.CreateThingGroupInput, optFns ...func(*iot.Options)) (*iot.CreateThingGroupOutput, error) {

	if m.ForceGroupCreationError {
		return nil, &smithy.GenericAPIError{
			Code:    "InternalFailure",
			Message: "Forced group creation error",
		}
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	groupName := *params.ThingGroupName

	// Create the group if it doesn't exist
	if _, exists := m.ThingGroups[groupName]; !exists {
		m.ThingGroups[groupName] = []string{}
	}

	// Track parent relationship
	if params.ParentGroupName != nil && *params.ParentGroupName != "" {
		m.ThingGroupParents[groupName] = *params.ParentGroupName
	}

	return &iot.CreateThingGroupOutput{
		ThingGroupArn:  aws.String(fmt.Sprintf("arn:aws:iot:us-west-2:123456789012:thinggroup/%s", groupName)),
		ThingGroupName: aws.String(groupName),
	}, nil
}

func (m *IoTClientMock) AddThingToThingGroup(ctx context.Context, params *iot.AddThingToThingGroupInput, optFns ...func(*iot.Options)) (*iot.AddThingToThingGroupOutput, error) {
	if m.ForceGroupAdditionError {
		return nil, &smithy.GenericAPIError{
			Code:    "InternalFailure",
			Message: "Forced group addition error",
		}
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	thingName := *params.ThingName
	groupName := *params.ThingGroupName

	// Check if the thing exists
	thing, exists := m.Things[thingName]
	if !exists {
		return nil, rmerror.NewRMError(fmt.Errorf("thing not found: %s", thingName), "")
	}

	// Create the group if it doesn't exist
	if _, exists := m.ThingGroups[groupName]; !exists {
		m.ThingGroups[groupName] = []string{}
	}

	// Add thing to group if it's not already a member
	isGroupMember := false
	for _, member := range m.ThingGroups[groupName] {
		if member == thingName {
			isGroupMember = true
			break
		}
	}
	if !isGroupMember {
		m.ThingGroups[groupName] = append(m.ThingGroups[groupName], thingName)
	}

	// Also update the thing's groups list
	isInThingGroups := false
	for _, g := range thing.Groups {
		if g == groupName {
			isInThingGroups = true
			break
		}
	}
	if !isInThingGroups {
		thing.Groups = append(thing.Groups, groupName)
		m.Things[thingName] = thing
	}

	return &iot.AddThingToThingGroupOutput{}, nil
}

func (m *IoTClientMock) AttachPolicy(ctx context.Context, params *iot.AttachPolicyInput, optFns ...func(*iot.Options)) (*iot.AttachPolicyOutput, error) {
	if m.ForcePolicyAttachmentError {
		return nil, &smithy.GenericAPIError{
			Code:    "InternalFailure",
			Message: "Forced policy attachment error",
		}
	}

	// Track which policies are attached to which targets
	target := *params.Target
	policyName := *params.PolicyName
	m.mutex.Lock()
	if m.AttachedPolicies == nil {
		m.AttachedPolicies = make(map[string][]string)
	}
	m.AttachedPolicies[target] = append(m.AttachedPolicies[target], policyName)
	m.mutex.Unlock()

	return &iot.AttachPolicyOutput{}, nil
}

// GetAttachedPolicies returns the list of policy names attached to a target (cert ARN).
func (m *IoTClientMock) GetAttachedPolicies(target string) []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if m.AttachedPolicies == nil {
		return nil
	}
	return m.AttachedPolicies[target]
}

func (m *IoTClientMock) DescribeThing(ctx context.Context, params *iot.DescribeThingInput, optFns ...func(*iot.Options)) (*iot.DescribeThingOutput, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	thingName := *params.ThingName
	thing, exists := m.Things[thingName]
	if !exists {
		return nil, &smithy.GenericAPIError{
			Code:    "ResourceNotFoundException",
			Message: fmt.Sprintf("Thing %s not found", thingName),
		}
	}

	// Copy attributes to avoid leaking the underlying map.
	attrs := make(map[string]string, len(thing.Attributes))
	for k, v := range thing.Attributes {
		attrs[k] = v
	}
	return &iot.DescribeThingOutput{
		ThingName:  aws.String(thingName),
		ThingArn:   aws.String(fmt.Sprintf("arn:aws:iot:us-west-2:123456789012:thing/%s", thingName)),
		Attributes: attrs,
	}, nil
}

func (m *IoTClientMock) DescribeThingGroup(ctx context.Context, params *iot.DescribeThingGroupInput, optFns ...func(*iot.Options)) (*iot.DescribeThingGroupOutput, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	groupName := *params.ThingGroupName

	if _, exists := m.ThingGroups[groupName]; !exists {
		return nil, &smithy.GenericAPIError{
			Code:    "ResourceNotFoundException",
			Message: fmt.Sprintf("Thing group %s not found", groupName),
		}
	}

	output := &iot.DescribeThingGroupOutput{
		ThingGroupArn:  aws.String(fmt.Sprintf("arn:aws:iot:us-west-2:123456789012:thinggroup/%s", groupName)),
		ThingGroupName: aws.String(groupName),
	}

	// Include parent metadata if a parent relationship exists
	if parentName, hasParent := m.ThingGroupParents[groupName]; hasParent {
		output.ThingGroupMetadata = &types.ThingGroupMetadata{
			ParentGroupName: aws.String(parentName),
		}
	}

	return output, nil
}

func (m *IoTClientMock) DeleteCertificate(ctx context.Context, params *iot.DeleteCertificateInput, optFns ...func(*iot.Options)) (*iot.DeleteCertificateOutput, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	certID := *params.CertificateId

	cert, exists := m.Certificates[certID]
	if !exists {
		return nil, &smithy.GenericAPIError{
			Code:    "ResourceNotFoundException",
			Message: fmt.Sprintf("Certificate '%s' not found", certID),
		}
	}

	if cert.Status != types.CertificateStatusInactive {
		return nil, &smithy.GenericAPIError{
			Code:    "InvalidStateException",
			Message: "Certificate must be in INACTIVE state before deletion",
		}
	}

	delete(m.Certificates, certID)
	return &iot.DeleteCertificateOutput{}, nil
}

func (m *IoTClientMock) DeleteThing(ctx context.Context, params *iot.DeleteThingInput, optFns ...func(*iot.Options)) (*iot.DeleteThingOutput, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	thingName := *params.ThingName

	// Check if thing exists before deleting
	if _, exists := m.Things[thingName]; !exists {
		return nil, &smithy.GenericAPIError{
			Code:    "ResourceNotFoundException",
			Message: fmt.Sprintf("Thing '%s' not found", thingName),
		}
	}

	delete(m.Things, thingName)
	return &iot.DeleteThingOutput{}, nil
}

func (m *IoTClientMock) DetachThingPrincipal(ctx context.Context, params *iot.DetachThingPrincipalInput, optFns ...func(*iot.Options)) (*iot.DetachThingPrincipalOutput, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	thingName := *params.ThingName
	principal := *params.Principal

	thing, exists := m.Things[thingName]
	if !exists {
		return nil, rmerror.NewRMError(fmt.Errorf("thing not found: %s", thingName), "")
	}

	// Extract cert ID from ARN
	certID, err := iotutil.ExtractCertIDFromARN(principal)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to extract certificate ID from ARN")
	}

	// Remove the certID from the thing's certificates
	newCerts := make([]string, 0)
	for _, id := range thing.CertificateIds {
		if id != certID {
			newCerts = append(newCerts, id)
		}
	}
	thing.CertificateIds = newCerts
	m.Things[thingName] = thing

	return &iot.DetachThingPrincipalOutput{}, nil
}

func (m *IoTClientMock) UpdateCertificate(ctx context.Context, params *iot.UpdateCertificateInput, optFns ...func(*iot.Options)) (*iot.UpdateCertificateOutput, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	certID := *params.CertificateId
	newStatus := params.NewStatus

	cert, exists := m.Certificates[certID]
	if !exists {
		return nil, rmerror.NewRMError(fmt.Errorf("certificate not found: %s", certID), "")
	}

	cert.Status = newStatus
	m.Certificates[certID] = cert

	return &iot.UpdateCertificateOutput{}, nil
}

func (m *IoTClientMock) UpdateThing(ctx context.Context, params *iot.UpdateThingInput, optFns ...func(*iot.Options)) (*iot.UpdateThingOutput, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	thingName := *params.ThingName
	thing, exists := m.Things[thingName]
	if !exists {
		return nil, &smithy.GenericAPIError{
			Code:    "ResourceNotFoundException",
			Message: fmt.Sprintf("Thing '%s' not found", thingName),
		}
	}

	if params.AttributePayload != nil {
		if thing.Attributes == nil {
			thing.Attributes = make(map[string]string)
		}
		for k, v := range params.AttributePayload.Attributes {
			thing.Attributes[k] = v
		}
		m.Things[thingName] = thing
	}

	return &iot.UpdateThingOutput{}, nil
}

// GetThingAttributeDirect retrieves a specific attribute value for a thing
func (m *IoTClientMock) GetThingAttributeDirect(thingName, attributeKey string) (string, bool) {
	thing, exists := m.Things[thingName]
	if !exists || thing.Attributes == nil {
		return "", false
	}
	val, ok := thing.Attributes[attributeKey]
	return val, ok
}

// SetTopicRuleDirect stores a topic rule payload by name for tests of the
// IoT-event-mode admin API.
func (m *IoTClientMock) SetTopicRuleDirect(ruleName string, payload *types.TopicRulePayload) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.TopicRules == nil {
		m.TopicRules = make(map[string]*types.TopicRulePayload)
	}
	m.TopicRules[ruleName] = payload
}

// GetTopicRuleDirect retrieves a stored topic rule payload by name.
func (m *IoTClientMock) GetTopicRuleDirect(ruleName string) (*types.TopicRulePayload, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	payload, ok := m.TopicRules[ruleName]
	return payload, ok
}

func (m *IoTClientMock) GetTopicRule(ctx context.Context, params *iot.GetTopicRuleInput, optFns ...func(*iot.Options)) (*iot.GetTopicRuleOutput, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if params == nil || params.RuleName == nil {
		return nil, &smithy.GenericAPIError{Code: "InvalidRequestException", Message: "RuleName is required"}
	}
	payload, ok := m.TopicRules[*params.RuleName]
	if !ok {
		return nil, &smithy.GenericAPIError{
			Code:    "ResourceNotFoundException",
			Message: fmt.Sprintf("Rule '%s' not found", *params.RuleName),
		}
	}
	return &iot.GetTopicRuleOutput{
		Rule: &types.TopicRule{
			RuleName:         params.RuleName,
			Sql:              payload.Sql,
			Description:      payload.Description,
			AwsIotSqlVersion: payload.AwsIotSqlVersion,
			RuleDisabled:     payload.RuleDisabled,
			Actions:          payload.Actions,
			ErrorAction:      payload.ErrorAction,
		},
	}, nil
}

func (m *IoTClientMock) ReplaceTopicRule(ctx context.Context, params *iot.ReplaceTopicRuleInput, optFns ...func(*iot.Options)) (*iot.ReplaceTopicRuleOutput, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if params == nil || params.RuleName == nil || params.TopicRulePayload == nil {
		return nil, &smithy.GenericAPIError{Code: "InvalidRequestException", Message: "RuleName and TopicRulePayload are required"}
	}
	if m.TopicRules == nil {
		m.TopicRules = make(map[string]*types.TopicRulePayload)
	}
	m.TopicRules[*params.RuleName] = params.TopicRulePayload
	return &iot.ReplaceTopicRuleOutput{}, nil
}
