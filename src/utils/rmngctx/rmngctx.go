// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmngctx

import (
	"context"
	"fmt"
	"github.com/espressif/esp-cloud-common/go/rbac/rbac"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"sync"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils"

	"github.com/rs/zerolog"
)

type AccessorEntity interface {
	GetID() string
	GetPermissions() *rbac.EntityPermissions
}

type RmngContext struct {
	Context  context.Context
	Accessor AccessorEntity
	// [action][resource]Condition
	// XXX this is hacked up. This expects that even if a wildcard condition matches in Permissions
	// we need to get exact match in ExtraConditions, if it exists for this combination
	ExtraConditions map[string]map[string][]Condition
	// A single context is shared across goroutines in parallel fan-outs (e.g. ShadowNodeRemoveFromGroupAuthorizedBulk), where each goroutine both reads (IsAuthorized) and writes (SetAllow) the same map
	permMu sync.RWMutex
	// condMu guards ExtraConditions. The sub-entity path writes it (SetScopedConditions) while the same fan-out reads it (IsConditionMatch), so it needs its own lock — a concurrent map write would otherwise panic. Kept separate from permMu as the two never nest.
	condMu sync.RWMutex
}

// conditionOperatorEqual is the only operator IsConditionMatch understands; anything else is a programming error and denies.
const conditionOperatorEqual = "equal"

type Condition struct {
	Operator string
	Value    string
}

// Deadline implements context.Context
func (c *RmngContext) Deadline() (deadline time.Time, ok bool) {
	if c == nil {
		return time.Time{}, false
	}
	return c.Context.Deadline()
}

// Done implements context.Context
func (c *RmngContext) Done() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.Context.Done()
}

// Err implements context.Context
func (c *RmngContext) Err() error {
	if c == nil {
		return nil
	}
	return c.Context.Err()
}

// Value implements context.Context
func (c *RmngContext) Value(key interface{}) interface{} {
	if c == nil || c.Context == nil {
		return nil
	}
	return c.Context.Value(key)
}

func NewRmngContextWithCtx(ctx context.Context, accessor AccessorEntity) *RmngContext {
	return &RmngContext{
		Context:  ctx,
		Accessor: accessor,
	}
}

func NewRmngContextWithUser(ctx context.Context, accessor AccessorEntity, uid string) *RmngContext {
	logger := utils.Rlog.With().Str("uid", uid).Logger()
	return &RmngContext{Context: logger.WithContext(ctx), Accessor: accessor}
}

func NewRmngContextWithNode(ctx context.Context, accessor AccessorEntity, nid string) *RmngContext {
	logger := utils.Rlog.With().Str("nid", nid).Logger()
	return &RmngContext{Context: logger.WithContext(ctx), Accessor: accessor}
}

// EnrichLogger adds a field to the logger stored in this context.
func (c *RmngContext) EnrichLogger(key, value string) {
	logger := zerolog.Ctx(c).With().Str(key, value).Logger()
	c.Context = logger.WithContext(c.Context)
}

// TODO: Why is this needed?
func NewRmngContext(accessor AccessorEntity) *RmngContext {
	return &RmngContext{
		Context:  context.Background(),
		Accessor: accessor,
	}
}

func (c *RmngContext) IsAuthorized(action fmt.Stringer, resource string) error {
	if c == nil {
		return rmerror.NewRMError(nil, "context is nil, cannot check authorization")
	}
	if c.Accessor == nil {
		return rmerror.NewRMError(nil, "accessor is nil, cannot check authorization")
	}
	entityID := c.Accessor.GetID()
	if entityID == "" {
		return rmerror.NewRMError(nil, fmt.Sprintf("entity ID is empty, cannot check authorization for action %s on resource %s", action, resource))
	}
	c.permMu.RLock()
	authorized := c.Accessor.GetPermissions().IsAuthorized(action.String(), resource)
	c.permMu.RUnlock()
	if !authorized {
		errstr := fmt.Sprintf("entity %s is not authorized to perform %s on %s", entityID, action, resource)
		return rmerror.NewRMError(nil, errstr)
	}
	return nil
}

// ensureConditionSlot creates the [action][resource] entry if absent. Caller must hold condMu.
func (c *RmngContext) ensureConditionSlot(action string, resource string) {
	if c.ExtraConditions == nil {
		c.ExtraConditions = make(map[string]map[string][]Condition)
	}
	if c.ExtraConditions[action] == nil {
		c.ExtraConditions[action] = make(map[string][]Condition)
	}
	if _, ok := c.ExtraConditions[action][resource]; !ok {
		c.ExtraConditions[action][resource] = []Condition{}
	}
}

// SetScopedConditions narrows (action, resource) to exactly values, so IsConditionMatch denies anything not listed. Registering the entry is the load-bearing part when values is empty: with no entry, IsConditionMatch reads the grant as unscoped and allows every value.
func (c *RmngContext) SetScopedConditions(action fmt.Stringer, resource string, values []string) error {
	if c == nil {
		return rmerror.NewRMError(nil, "context is nil, cannot set scoped conditions")
	}
	c.condMu.Lock()
	defer c.condMu.Unlock()
	c.ensureConditionSlot(action.String(), resource)
	for _, value := range values {
		c.ExtraConditions[action.String()][resource] = append(c.ExtraConditions[action.String()][resource], Condition{
			Operator: conditionOperatorEqual,
			Value:    value,
		})
	}
	return nil
}

// IsConditionMatch reports whether value satisfies the extra conditions recorded for
// (action, resource). An absent condition means "unrestricted", not "denied": full-group
// access deliberately sets no conditions, so defaulting to deny here would hide every
// subgroup from a primary/secondary user. Callers that narrow access must therefore set
// their restriction when they grant the permission — see GetUserSubGroup and
// ListGroupsForUser in db/user_group_db.go.
func (c *RmngContext) IsConditionMatch(action fmt.Stringer, resource string, value string) (bool, error) {
	if c == nil {
		return false, rmerror.NewRMError(nil, "context is nil, cannot check condition match")
	}
	c.condMu.RLock()
	defer c.condMu.RUnlock()
	// No entry means the grant was never scoped (primary/secondary access covers every sub-entity), so allow. Scoped grants go through SetScopedConditions, which always registers an entry so an unmatched value falls through to the deny below.
	if c.ExtraConditions == nil {
		return true, nil
	}
	extraConditions, ok := c.ExtraConditions[action.String()][resource]
	if !ok {
		return true, nil
	}
	for _, condition := range extraConditions {
		switch condition.Operator {
		case conditionOperatorEqual:
			if condition.Value == value {
				return true, nil
			}
		default:
			return false, fmt.Errorf("unsupported operator: %s", condition.Operator)
		}
	}
	return false, nil
}

func (c *RmngContext) SetAllow(action fmt.Stringer, resource string) error {
	if c == nil {
		return rmerror.NewRMError(nil, "context is nil, cannot set allow permission")
	}
	if c.Accessor == nil {
		return rmerror.NewRMError(nil, "accessor is nil, cannot set allow permission")
	}
	c.permMu.Lock()
	c.Accessor.GetPermissions().SetAllow(action.String(), resource)
	c.permMu.Unlock()
	return nil
}

func (c *RmngContext) SetAllowMultiple(actions []string, resource string) error {
	if c == nil {
		return rmerror.NewRMError(nil, "context is nil, cannot set allow permissions")
	}
	if c.Accessor == nil {
		return rmerror.NewRMError(nil, "accessor is nil, cannot set allow permissions")
	}
	c.permMu.Lock()
	for _, action := range actions {
		c.Accessor.GetPermissions().SetAllow(action, resource)
	}
	c.permMu.Unlock()
	return nil
}

func (c *RmngContext) GetID() string {
	if c == nil {
		return ""
	}
	if c.Accessor == nil {
		return ""
	}
	return c.Accessor.GetID()
}

func (c *RmngContext) GetAccessor() AccessorEntity {
	if c == nil {
		return nil
	}

	return c.Accessor
}
