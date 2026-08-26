// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// Validating a params write against the node's own config exists for one caller in particular: an
// LLM driving the MCP server. set_params is a generic write — {"<device>": {"<param>": value}} —
// so a model can name a parameter the device never declared, and until this check existed the
// cloud accepted it, published it to a device that ignores unknown keys, and reported success.
// There is no acknowledgement from the device, so nothing surfaced later either.
//
// The governing rule throughout: reject only when the config *positively contradicts* the write.
// Absence of metadata is permission. Config is reported by firmware and its ingest is never
// schema-checked, so sparse and malformed configs demonstrably exist; a validator that treated
// silence as prohibition would make working devices uncontrollable, which is a far worse failure
// than the one it is fixing.

// ViolationKind classifies why a write was refused. Callers use it for metrics; the message a
// model sees is rendered from the Violation itself.
type ViolationKind string

const (
	ViolationUnknownDevice ViolationKind = "unknown_device"
	ViolationUnknownParam  ViolationKind = "unknown_param"
	ViolationReadOnly      ViolationKind = "read_only"
	ViolationWrongType     ViolationKind = "wrong_type"
	ViolationOutOfBounds   ViolationKind = "out_of_bounds"
	ViolationBadShape      ViolationKind = "bad_shape"
)

// Violation is one reason a write was refused. It carries the alternatives so the caller can
// render an actionable message without walking the config a second time.
type Violation struct {
	Kind ViolationKind
	// Device is the top-level key of the params map: a device or service id.
	Device string
	// Param is empty for device-level violations.
	Param string
	// Value is the offending value as decoded, for the message.
	Value interface{}
	// Expected describes what would have been accepted: "a boolean (true/false)", "0-100".
	Expected string
	// DidYouMean is set when the caller's key differs from a real one only by case or spacing.
	DidYouMean string
	// Allowed lists the alternatives at whichever level failed, already rendered.
	Allowed []string
}

const (
	propertyWrite = "write"

	// Caps on what a rendered message carries. A model reads these; an unbounded list of 40
	// parameters buys nothing and crowds out the rest of its context.
	maxViolationsShown = 3
	maxParamsShown     = 12
	maxMessageLen      = 400
)

// dataType values this validator understands. Anything else — a typo, "object", a future
// addition — is unjudgeable and skipped rather than guessed at.
const (
	dataTypeBool   = "bool"
	dataTypeInt    = "int"
	dataTypeFloat  = "float"
	dataTypeString = "string"
	dataTypeArray  = "array"
)

// SkipValidation reports whether this config carries too little to judge any write at all.
//
// Matter nodes are the important case: they describe themselves as Endpoints of hex cluster and
// attribute ids, with no per-attribute type metadata and no device names, so a device-name lookup
// would reject every legitimate write rather than catching a bad one.
func (nc NodeCfg) SkipValidation() bool {
	if strings.EqualFold(nc.DataModel, "matter") {
		return true
	}
	return len(nc.Devices) == 0 && len(nc.Services) == 0
}

// FindDevice returns the device with the given id.
func (nc NodeCfg) FindDevice(deviceID string) (*NodeCfgDevice, bool) {
	for index := range nc.Devices {
		if nc.Devices[index].ID == deviceID {
			return &nc.Devices[index], true
		}
	}
	return nil, false
}

// FindService returns the service with the given id.
func (nc NodeCfg) FindService(serviceID string) (*NodeCfgService, bool) {
	for index := range nc.Services {
		if nc.Services[index].ID == serviceID {
			return &nc.Services[index], true
		}
	}
	return nil, false
}

// ParamsFor returns the params declared under a device *or* a service id, and the id of that
// entry's primary param.
//
// Services matter as much as devices here: Reboot, Network-Reset and Factory-Reset live under a
// service (conventionally id "System"), not under Devices, and set_params exists to reach them.
// Walking Devices alone would reject the reboot the tool advertises.
func (nc NodeCfg) ParamsFor(id string) (params []NodeCfgDeviceParam, primary string, found bool) {
	if device, ok := nc.FindDevice(id); ok {
		return device.Params, device.Primary, true
	}
	if svc, ok := nc.FindService(id); ok {
		return svc.Params, svc.Primary, true
	}
	return nil, "", false
}

// FindParam returns a param by exact id, looking in devices and services alike.
func (nc NodeCfg) FindParam(deviceID, paramID string) (*NodeCfgDeviceParam, bool) {
	params, _, found := nc.ParamsFor(deviceID)
	if !found {
		return nil, false
	}
	for index := range params {
		if params[index].ID == paramID {
			return &params[index], true
		}
	}
	return nil, false
}

// FindParamByType returns the first param of a given semantic type — esp.param.hue and the like.
// The bare form ("hue") is accepted too, because that is how the voice integrations spell it.
//
// This is the lookup that makes a generic write usable: a param's id is whatever firmware chose
// (a colour light may call its hue "H"), but its type is stable across every device.
func (nc NodeCfg) FindParamByType(deviceID, paramType string) (*NodeCfgDeviceParam, bool) {
	params, _, found := nc.ParamsFor(deviceID)
	if !found {
		return nil, false
	}
	wanted := strings.ToLower(strings.TrimPrefix(paramType, espParamPrefix))
	for index := range params {
		if strings.ToLower(strings.TrimPrefix(params[index].Type, espParamPrefix)) == wanted {
			return &params[index], true
		}
	}
	return nil, false
}

const espParamPrefix = "esp.param."

// IsWritable reports whether a param may be written. An absent or empty properties list reads as
// writable: firmware routinely omits it, and only "time_series" is consumed anywhere else, so
// treating silence as read-only would refuse most valid writes.
func (p NodeCfgDeviceParam) IsWritable() bool {
	if len(p.Properties) == 0 {
		return true
	}
	for _, property := range p.Properties {
		if strings.EqualFold(strings.TrimSpace(property), propertyWrite) {
			return true
		}
	}
	return false
}

// SpecDetail describes what a param accepts: `int 0-360, hue`. The data type and the range are
// dropped when the config does not declare them, and the semantic type comes last because it is
// the hint, not the identifier — a colour light may call its hue "H", but its type is always
// esp.param.hue, and that is what lets a model map an intent onto an arbitrary name.
func (p NodeCfgDeviceParam) SpecDetail() string {
	var detail []string
	if measure := p.rangeSpec(); measure != "" {
		detail = append(detail, measure)
	} else if p.DataType != "" {
		detail = append(detail, p.DataType)
	}
	if short := strings.TrimPrefix(p.Type, espParamPrefix); short != "" && short != p.Type {
		detail = append(detail, short)
	}
	return strings.Join(detail, ", ")
}

// Spec renders one param the way a model should see it in an error: `H (int 0-360, hue)`.
func (p NodeCfgDeviceParam) Spec() string {
	detail := p.SpecDetail()
	if detail == "" {
		return p.ID
	}
	return fmt.Sprintf("%s (%s)", p.ID, detail)
}

// rangeSpec renders "int 0-100" when both sides of a usable bound are declared, "int 0+" or
// "int up to 100" when only one is.
func (p NodeCfgDeviceParam) rangeSpec() string {
	min, max, usable := p.bounds()
	if !usable {
		return ""
	}
	dataType := p.DataType
	if dataType == "" {
		dataType = "number"
	}
	switch {
	case min != nil && max != nil:
		return fmt.Sprintf("%s %g-%g", dataType, *min, *max)
	case min != nil:
		return fmt.Sprintf("%s %g or more", dataType, *min)
	default:
		return fmt.Sprintf("%s up to %g", dataType, *max)
	}
}

// bounds returns the usable half-bounds in float64 space, which is also how values are compared:
// Bounds is declared *int, so comparing as floats is what lets an int bound constrain a float
// param without changing a struct the voice integrations already read.
//
// Degenerate bounds are discarded rather than enforced. Config ingest is unchecked, so min==max==0
// reaches storage from buggy firmware, and honouring it would refuse every write to a live device.
func (p NodeCfgDeviceParam) bounds() (min, max *float64, usable bool) {
	if p.Bounds == nil {
		return nil, nil, false
	}
	if p.Bounds.Min != nil {
		value := float64(*p.Bounds.Min)
		min = &value
	}
	if p.Bounds.Max != nil {
		value := float64(*p.Bounds.Max)
		max = &value
	}
	if min == nil && max == nil {
		return nil, nil, false
	}
	if min != nil && max != nil && *min >= *max {
		return nil, nil, false
	}
	return min, max, true
}

// WritableParams returns a device's or service's writable params, primary first. Config order is
// otherwise preserved because it mirrors what the app shows, which puts the params a user is
// likely to mean near the front.
func (nc NodeCfg) WritableParams(deviceID string) []NodeCfgDeviceParam {
	params, primary, found := nc.ParamsFor(deviceID)
	if !found {
		return nil
	}
	writable := make([]NodeCfgDeviceParam, 0, len(params))
	for _, param := range params {
		if param.IsWritable() {
			writable = append(writable, param)
		}
	}
	if primary != "" {
		sort.SliceStable(writable, func(i, j int) bool {
			return writable[i].ID == primary && writable[j].ID != primary
		})
	}
	return writable
}

// WritableIDs returns just the ids, for callers that want names without specs.
func (nc NodeCfg) WritableIDs(deviceID string) []string {
	params := nc.WritableParams(deviceID)
	ids := make([]string, 0, len(params))
	for _, param := range params {
		ids = append(ids, param.ID)
	}
	return ids
}

// entryIDs lists every top-level key a params map may legitimately use.
func (nc NodeCfg) entryIDs() []string {
	ids := make([]string, 0, len(nc.Devices)+len(nc.Services))
	for _, device := range nc.Devices {
		ids = append(ids, device.ID)
	}
	for _, svc := range nc.Services {
		ids = append(ids, svc.ID)
	}
	return ids
}

// ValidateParams checks a set_params-shaped payload against what the node declared.
//
// It never mutates params, and it never coerces: a caller publishes its own map or nothing at all.
// Coercing "80" to 80 would turn this into a transform every caller had to adopt, and would put
// back the silent failure it exists to remove — firmware does not coerce, so a coerced-looking
// success would still be a message the device drops.
//
// A nil result means the config does not contradict the write, which is also what a config too
// sparse to judge returns.
func (nc NodeCfg) ValidateParams(params map[string]interface{}) []Violation {
	if nc.SkipValidation() {
		return nil
	}

	var violations []Violation
	// Deterministic order: a model retrying the same bad call must see the same message, and the
	// tests would otherwise be flaky against Go's randomised map iteration.
	for _, deviceID := range sortedKeys(params) {
		violations = append(violations, nc.validateDevice(deviceID, params[deviceID])...)
	}
	return violations
}

func (nc NodeCfg) validateDevice(deviceID string, raw interface{}) []Violation {
	declared, _, found := nc.ParamsFor(deviceID)
	if !found {
		return []Violation{{
			Kind:       ViolationUnknownDevice,
			Device:     deviceID,
			DidYouMean: nearMiss(deviceID, nc.entryIDs()),
			Allowed:    nc.entryIDs(),
		}}
	}

	// The wire shape is always device -> param -> value. {"Light": true} and {"Light": {}} both
	// publish nothing a device can act on while still reporting success, which is the class of
	// failure this whole check exists to end.
	paramValues, ok := raw.(map[string]interface{})
	if !ok {
		return []Violation{{
			Kind:     ViolationBadShape,
			Device:   deviceID,
			Value:    raw,
			Expected: fmt.Sprintf(`a parameter object, for example {%q: {%q: ...}}`, deviceID, firstID(declared, "Power")),
		}}
	}
	if len(paramValues) == 0 {
		return []Violation{{
			Kind:     ViolationBadShape,
			Device:   deviceID,
			Expected: "at least one parameter to set",
			Allowed:  specsOf(nc.WritableParams(deviceID)),
		}}
	}

	// The device name was worth checking on its own; its params are not judgeable.
	if len(declared) == 0 {
		return nil
	}

	var violations []Violation
	for _, paramID := range sortedKeys(paramValues) {
		if violation, bad := nc.validateParam(deviceID, paramID, paramValues[paramID]); bad {
			violations = append(violations, violation)
		}
	}
	return violations
}

func (nc NodeCfg) validateParam(deviceID, paramID string, value interface{}) (Violation, bool) {
	writable := nc.WritableParams(deviceID)

	param, found := nc.FindParam(deviceID, paramID)
	if !found {
		declared, _, _ := nc.ParamsFor(deviceID)
		return Violation{
			Kind:       ViolationUnknownParam,
			Device:     deviceID,
			Param:      paramID,
			DidYouMean: nearMiss(paramID, idsOf(declared)),
			Allowed:    specsOf(writable),
		}, true
	}

	if !param.IsWritable() {
		return Violation{
			Kind:     ViolationReadOnly,
			Device:   deviceID,
			Param:    paramID,
			Expected: "read-only",
			Allowed:  specsOf(writable),
		}, true
	}

	// A null value is left alone: it is rare, may be a clear-the-value idiom, and is not worth
	// refusing a write over.
	if value == nil {
		return Violation{}, false
	}

	if expected, ok := param.typeMismatch(value); !ok {
		return Violation{
			Kind:     ViolationWrongType,
			Device:   deviceID,
			Param:    paramID,
			Value:    value,
			Expected: expected,
		}, true
	}

	if expected, ok := param.outOfBounds(value); !ok {
		return Violation{
			Kind:     ViolationOutOfBounds,
			Device:   deviceID,
			Param:    paramID,
			Value:    value,
			Expected: expected,
		}, true
	}

	return Violation{}, false
}

// typeMismatch checks a value against the declared data_type, returning what was expected when it
// does not fit. An undeclared or unrecognised data_type is unjudgeable, so it passes.
func (p NodeCfgDeviceParam) typeMismatch(value interface{}) (expected string, ok bool) {
	switch p.DataType {
	case dataTypeBool:
		if _, isBool := value.(bool); !isBool {
			return "a boolean (true/false)", false
		}
	case dataTypeString:
		if _, isString := value.(string); !isString {
			return "a string", false
		}
	case dataTypeInt:
		number, isNumber := asFloat(value)
		if !isNumber {
			return "a whole number", false
		}
		// JSON has one number type, so 80 and 80.0 decode identically; integrality has to be
		// judged on the value, never on how it was written. 80.5 genuinely cannot round-trip.
		if math.Trunc(number) != number {
			return "a whole number", false
		}
	case dataTypeFloat:
		if _, isNumber := asFloat(value); !isNumber {
			return "a number", false
		}
	case dataTypeArray:
		if _, isArray := value.([]interface{}); !isArray {
			return "an array", false
		}
	}
	return "", true
}

// outOfBounds range-checks any numeric value that has usable bounds. It keys off the value being
// numeric rather than off data_type, because plenty of configs carry bounds while omitting the type.
func (p NodeCfgDeviceParam) outOfBounds(value interface{}) (expected string, ok bool) {
	number, isNumber := asFloat(value)
	if !isNumber {
		return "", true
	}
	min, max, usable := p.bounds()
	if !usable {
		return "", true
	}
	if min != nil && number < *min {
		return p.rangeSpec(), false
	}
	if max != nil && number > *max {
		return p.rangeSpec(), false
	}
	return "", true
}

// asFloat accepts the float64 every JSON number decodes to, plus the Go integer types a
// hand-built map may carry. NaN and infinities are not numbers any device can use.
func asFloat(value interface{}) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int32:
		number = float64(typed)
	case int64:
		number = float64(typed)
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

// ViolationsMessage renders violations into the single line a model is shown. It names the
// alternatives at whichever level failed, suggests a near miss when there is one, and closes by
// saying plainly that nothing changed — a model that is not told that will report success.
func ViolationsMessage(nodeID string, violations []Violation) string {
	if len(violations) == 0 {
		return ""
	}

	shown := violations
	var overflow int
	if len(shown) > maxViolationsShown {
		overflow = len(shown) - maxViolationsShown
		shown = shown[:maxViolationsShown]
	}

	sentences := make([]string, 0, len(shown))
	for _, violation := range shown {
		sentences = append(sentences, violation.sentence(nodeID))
	}

	message := strings.Join(sentences, "; ")
	if overflow > 0 {
		message += fmt.Sprintf(" (+%d more)", overflow)
	}
	message += fmt.Sprintf(". Nothing was changed on node %s.", nodeID)

	// Never auto-correct a near miss into the write. Rewriting "power" to "Power" would put
	// guessing back inside the one component whose job is to remove it.
	if hasKind(violations, ViolationUnknownParam, ViolationUnknownDevice) {
		message += " If none of these do what the user asked, tell them this device does not support it."
	}
	return truncate(message, maxMessageLen)
}

func (v Violation) sentence(nodeID string) string {
	switch v.Kind {
	case ViolationUnknownDevice:
		return fmt.Sprintf("node %s has no device or service named %q%s — it has: %s",
			nodeID, v.Device, v.didYouMean(), renderList(v.Allowed))
	case ViolationUnknownParam:
		return fmt.Sprintf("%q is not a parameter of %q%s — writable parameters: %s",
			v.Param, v.Device, v.didYouMean(), renderList(v.Allowed))
	case ViolationReadOnly:
		return fmt.Sprintf("%q on %q is read-only — writable parameters: %s",
			v.Param, v.Device, renderList(v.Allowed))
	case ViolationWrongType:
		return fmt.Sprintf("%q on %q expects %s, got %s", v.Param, v.Device, v.Expected, renderValue(v.Value))
	case ViolationOutOfBounds:
		return fmt.Sprintf("%q on %q accepts %s, got %s", v.Param, v.Device, v.Expected, renderValue(v.Value))
	case ViolationBadShape:
		return fmt.Sprintf("%q needs %s", v.Device, v.Expected)
	default:
		return fmt.Sprintf("%q on %q was rejected", v.Param, v.Device)
	}
}

func (v Violation) didYouMean() string {
	if v.DidYouMean == "" {
		return ""
	}
	return fmt.Sprintf(" — did you mean %q?", v.DidYouMean)
}

// Error lets a Violation travel as an error where a caller wants one.
func (v Violation) Error() string { return v.sentence("this node") }

// nearMiss finds a candidate that differs only in case or surrounding space. Anything looser
// would be a guess, and a wrong suggestion is worse than none.
func nearMiss(wanted string, candidates []string) string {
	normalise := func(value string) string {
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
	}
	target := normalise(wanted)
	for _, candidate := range candidates {
		if candidate != wanted && normalise(candidate) == target {
			return candidate
		}
	}
	return ""
}

// renderList caps the alternatives shown and says where to get the rest.
func renderList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	if len(values) <= maxParamsShown {
		return strings.Join(values, ", ")
	}
	return fmt.Sprintf("%s … +%d more — call list_devices for the full list",
		strings.Join(values[:maxParamsShown], ", "), len(values)-maxParamsShown)
}

func renderValue(value interface{}) string {
	if text, isString := value.(string); isString {
		return fmt.Sprintf("%q", text)
	}
	return fmt.Sprintf("%v", value)
}

func specsOf(params []NodeCfgDeviceParam) []string {
	specs := make([]string, 0, len(params))
	for _, param := range params {
		specs = append(specs, param.Spec())
	}
	return specs
}

func idsOf(params []NodeCfgDeviceParam) []string {
	ids := make([]string, 0, len(params))
	for _, param := range params {
		ids = append(ids, param.ID)
	}
	return ids
}

func firstID(params []NodeCfgDeviceParam, fallback string) string {
	if len(params) == 0 {
		return fallback
	}
	return params[0].ID
}

func hasKind(violations []Violation, kinds ...ViolationKind) bool {
	for _, violation := range violations {
		for _, kind := range kinds {
			if violation.Kind == kind {
				return true
			}
		}
	}
	return false
}

func sortedKeys(values map[string]interface{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// truncate keeps a message within limit *bytes*, ellipsis included. Both the ellipsis and the em
// dashes in these messages are multi-byte, so the cut has to be counted in bytes and moved back
// to a rune boundary — slicing mid-rune would emit replacement characters into the model's input.
func truncate(message string, limit int) string {
	if len(message) <= limit {
		return message
	}
	const ellipsis = "…"
	cut := limit - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut] + ellipsis
}

// ParamSpec summarises a device's writable params for a model: parameter id to a short
// description of what it accepts. It is built from the same NodeCfg that ValidateParams consults,
// which is the point — a model told about a parameter here must not then be refused for using it.
func (nc NodeCfg) ParamSpec() map[string]map[string]string {
	spec := make(map[string]map[string]string, len(nc.Devices)+len(nc.Services))
	for _, id := range nc.entryIDs() {
		params := nc.WritableParams(id)
		if len(params) == 0 {
			continue
		}
		entry := make(map[string]string, len(params))
		for _, param := range params {
			detail := param.SpecDetail()
			if detail == "" {
				// Declared, writable, but the config says nothing about what it takes.
				detail = "any value"
			}
			entry[param.ID] = detail
		}
		spec[id] = entry
	}
	if len(spec) == 0 {
		return nil
	}
	return spec
}
