// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package params owns the params write path: what a node accepts, the repair of a payload that obviously meant something acceptable, and the publish. Not a NodeService and not registered — params live in the device's shadow, not in the node's service data.
package params

import (
	"sort"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

type ParamsService struct {
	configService *config.ConfigService
}

func NewParamsService() *ParamsService {
	return &ParamsService{configService: config.NewConfigService()}
}

// Check returns the payload to publish — repaired where an obvious type mistake could be repaired — and the message to hand back to the caller, "" when the write may proceed. A device never acknowledges a params write, so this is the only place one it cannot act on can be caught.
func (s *ParamsService) Check(rmngCtx *rmngctx.RmngContext, nodeID string, params map[string]interface{}) (map[string]interface{}, string) {
	// Config is firmware-reported and never schema-checked, so anything that cannot judge the write lets it through: refusing on absent metadata would make working devices uncontrollable.
	skip := func(reason string) (map[string]interface{}, string) {
		rlog.Debug(rmngCtx).Str("node_id", nodeID).Str("validation_skipped", reason).
			Msg("Publishing params without validating them against node config")
		return params, ""
	}

	nodeDetails, err := node_details_db.NewNodeDetailsDB(rmngCtx).GetNodeDetails(nodeID)
	if err != nil || nodeDetails == nil {
		return skip("config_unreadable")
	}
	cfgData, err := nodeDetails.GetServiceData(s.configService.GetName())
	if err != nil || cfgData == nil {
		return skip("config_absent")
	}
	nodeCfg, err := config.ToNodeCfg(cfgData)
	if err != nil {
		return skip("config_undecodable")
	}
	if nodeCfg.SkipValidation() {
		return skip("config_not_judgeable")
	}

	// The repaired map is what callers must publish: validating the repair and publishing the original would report a success the device drops.
	payload, repaired := Repair(nodeCfg, params)
	if len(repaired) > 0 {
		rlog.Trace(rmngCtx).Str("node_id", nodeID).Strs("params_repaired", repaired).
			Msg("Repaired param values to the types the node declared")
	}

	violations := nodeCfg.ValidateParams(payload)
	if len(violations) == 0 {
		return payload, ""
	}
	for _, violation := range violations {
		rlog.Debug(rmngCtx).Str("node_id", nodeID).Str("validation_rejected", string(violation.Kind)).
			Str("device", violation.Device).Str("param", violation.Param).
			Msg("Refused a params write the node's config contradicts")
	}
	return payload, config.ViolationsMessage(nodeID, violations)
}

// Publish checks a payload and publishes the checked one. Exactly one return is ever set: guidance may be repeated to a user or a model, err is internal detail that must not travel outward.
func (s *ParamsService) Publish(rmngCtx *rmngctx.RmngContext, target *node.Node, params map[string]interface{}) (guidance string, err error) {
	payload, message := s.Check(rmngCtx, target.ThingName, params)
	if message != "" {
		return message, nil
	}
	if err := target.PublishToDeviceDesired(rmngCtx, payload); err != nil {
		return "", err
	}
	return "", nil
}

// Repair returns params with values fixed to the type the node declared, plus the "<device>.<param>" keys it changed, for the log. Small models quote booleans and retry the identical payload instead of reading the type out of a rejection, and firmware refuses an update of any other type (ESP_RMAKER_INVALID_ARG), so an unambiguous mistake is repaired rather than refused.
//
// It never repairs a value into acceptability: "150" for a 0-100 param becomes 150 and the bounds check still refuses it. The input map is never mutated, because a write fans out over nodes concurrently and repairs against each node's own config.
func Repair(nodeCfg config.NodeCfg, params map[string]interface{}) (map[string]interface{}, []string) {
	if nodeCfg.SkipValidation() {
		return params, nil
	}

	var repaired map[string]interface{}
	var changed []string
	for deviceID, raw := range params {
		values, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		var replacements map[string]interface{}
		for paramID, value := range values {
			param, found := nodeCfg.FindParam(deviceID, paramID)
			if !found {
				continue
			}
			replacement, needed := repairValue(*param, value)
			if !needed {
				continue
			}
			if replacements == nil {
				replacements = copyParams(values)
			}
			replacements[paramID] = replacement
			changed = append(changed, deviceID+"."+paramID)
		}
		if replacements == nil {
			continue
		}
		if repaired == nil {
			repaired = copyParams(params)
		}
		repaired[deviceID] = replacements
	}

	if repaired == nil {
		return params, nil
	}
	// Deterministic, so a retried call logs the same line.
	sort.Strings(changed)
	return repaired, changed
}

// repairValue reports the value to write and whether it differs from what was sent. An undeclared data_type is no basis for a repair, so it is left for the validator.
func repairValue(param config.NodeCfgDeviceParam, value interface{}) (interface{}, bool) {
	switch param.DataType {
	case config.DataTypeBool:
		return repairBool(value)
	case config.DataTypeInt, config.DataTypeFloat:
		return repairNumber(param.DataType, value)
	}
	return nil, false
}

func repairBool(value interface{}) (interface{}, bool) {
	if _, already := value.(bool); already {
		return nil, false
	}
	// A string that is not a spelling of a boolean — an enum value, a colour name — and a number that is not 1 or 0 report false and are left for the validator.
	flag, readable := utils.ToBool(value)
	if !readable {
		return nil, false
	}
	return flag, true
}

func repairNumber(dataType string, value interface{}) (interface{}, bool) {
	text, isString := value.(string)
	if !isString {
		return nil, false
	}
	number, readable := utils.ParseNumber(text)
	if !readable {
		return nil, false
	}
	if dataType == config.DataTypeInt {
		// 50.5 is not a value the caller can be assumed to have meant as 50, so the validator answers it instead.
		return utils.ToInt64(number)
	}
	return number, true
}

func copyParams(values map[string]interface{}) map[string]interface{} {
	copied := make(map[string]interface{}, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
