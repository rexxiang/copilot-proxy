package settingsform

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	appsettings "copilot-proxy/cmd/copilot-proxy/app/settings"
	"copilot-proxy/internal/core/runtimeconfig"
)

type FieldWidget string

const (
	WidgetText     FieldWidget = "text"
	WidgetURL      FieldWidget = "url"
	WidgetInt      FieldWidget = "int"
	WidgetBool     FieldWidget = "bool"
	WidgetDuration FieldWidget = "duration"
	WidgetKeyValue FieldWidget = "kv"
	WidgetArray    FieldWidget = "array"
)

var (
	errInvalidWidgetType  = errors.New("invalid widget type")
	errFieldNotFound      = errors.New("settings field not found")
	errInvalidURL         = errors.New("invalid url")
	errInvalidInt         = errors.New("invalid integer")
	errInvalidBool        = errors.New("invalid bool")
	errInvalidEnum        = errors.New("invalid enum value")
	errInvalidMinMax      = errors.New("invalid min/max")
	errDuplicateHeaderKey = errors.New("duplicate header key")
	errEmptyHeaderKey     = errors.New("empty header key")
	errReadonlyModified   = errors.New("readonly field modified")
	errInvalidWidget      = errors.New("invalid widget")
)

type Duration = runtimeconfig.Duration

var (
	NewDuration            = runtimeconfig.NewDuration
	ErrDurationEmpty       = runtimeconfig.ErrDurationEmpty
	ErrDurationNegative    = runtimeconfig.ErrDurationNegative
	ErrDurationWholeSeconds = runtimeconfig.ErrDurationWholeSeconds
	ErrInvalidDuration     = runtimeconfig.ErrInvalidDuration
)

type FieldSpec struct {
	FieldName   string
	Key         string
	Label       string
	Widget      FieldWidget
	Visible     bool
	ReadOnly    bool
	Order       int
	Placeholder string
	Min         string
	Max         string
	Description string
	EnumValues  []string
	EmptyZero   bool
	ElementType reflect.Type
	ElementSpec []FieldSpec
}

type HeaderKV struct {
	Key   string
	Value string
}

type SettingsForm struct {
	ScalarValues      map[string]string
	KeyValueValues    map[string][]HeaderKV
	ObjectArrayValues map[string][]map[string]string
}

func (f SettingsForm) Clone() SettingsForm {
	clone := SettingsForm{
		ScalarValues:      make(map[string]string, len(f.ScalarValues)),
		KeyValueValues:    make(map[string][]HeaderKV, len(f.KeyValueValues)),
		ObjectArrayValues: make(map[string][]map[string]string, len(f.ObjectArrayValues)),
	}
	for key, value := range f.ScalarValues {
		clone.ScalarValues[key] = value
	}
	for key, rows := range f.KeyValueValues {
		clonedRows := make([]HeaderKV, len(rows))
		copy(clonedRows, rows)
		clone.KeyValueValues[key] = clonedRows
	}
	for key, rows := range f.ObjectArrayValues {
		clonedRows := make([]map[string]string, 0, len(rows))
		for _, row := range rows {
			cloned := make(map[string]string, len(row))
			for rowKey, rowValue := range row {
				cloned[rowKey] = rowValue
			}
			clonedRows = append(clonedRows, cloned)
		}
		clone.ObjectArrayValues[key] = clonedRows
	}
	return clone
}

func SettingsFieldSpecs() ([]FieldSpec, error) {
	specs := []FieldSpec{
		{
			FieldName: "ListenAddr",
			Key:       "listen_addr",
			Label:     "Listen",
			Widget:    WidgetText,
			Visible:   false,
			ReadOnly:  true,
			Order:     10,
		},
		{
			FieldName: "UpstreamBase",
			Key:       "upstream_base",
			Label:     "Upstream",
			Widget:    WidgetURL,
			Visible:   true,
			ReadOnly:  true,
			Order:     20,
		},
		{
			FieldName: "MaxRetries",
			Key:       "max_retries",
			Label:     "Retries",
			Widget:    WidgetInt,
			Visible:   true,
			ReadOnly:  false,
			Order:     40,
			Min:       "1",
			Description: "Max upstream retry attempts.",
		},
		{
			FieldName: "RetryBackoff",
			Key:       "retry_backoff",
			Label:     "Backoff",
			Widget:    WidgetDuration,
			Visible:   true,
			ReadOnly:  false,
			Order:     50,
			Description: "Initial retry delay.",
		},
		{
			FieldName: "RateLimitSeconds",
			Key:       "rate_limit_seconds",
			Label:     "Rate Limit (sec)",
			Widget:    WidgetInt,
			Visible:   true,
			ReadOnly:  false,
			Order:     52,
			Min:       "0",
			Placeholder: "0",
			EmptyZero: true,
			Description: "Cooldown seconds between completed requests. 0 or empty disables it.",
		},
		{
			FieldName: "MessagesAgentDetectionRequestMode",
			Key:       "messages_agent_detection_request_mode",
			Label:     "Msg Agent Mode",
			Widget:    WidgetBool,
			Visible:   true,
			ReadOnly:  false,
			Order:     55,
		},
		{
			FieldName: "RequiredHeaders",
			Key:       "required_headers",
			Label:     "Headers",
			Widget:    WidgetKeyValue,
			Visible:   false,
			ReadOnly:  false,
			Order:     60,
		},
		{
			FieldName: "ReasoningPoliciesMap",
			Key:       "reasoning_policies",
			Label:     "ReasoningPoliciesMap",
			Widget:    WidgetKeyValue,
			Visible:   false,
			ReadOnly:  false,
			Order:     65,
		},
		{
			FieldName: "ReasoningPolicies",
			Key:       "reasoning_policies_ui",
			Label:     "Reasoning Policies",
			Widget:    WidgetArray,
			Visible:   true,
			ReadOnly:  false,
			Order:     66,
			Description: "Per-model reasoning policies.",
			ElementType: reflect.TypeOf(appsettings.ReasoningPolicy{}),
			ElementSpec: []FieldSpec{
				{FieldName: "Model", Key: "model", Label: "Model", Widget: WidgetText, Visible: true, ReadOnly: false, Order: 10, Placeholder: "gpt-5-mini"},
				{FieldName: "Target", Key: "target", Label: "Target", Widget: WidgetText, Visible: true, ReadOnly: false, Order: 20, EnumValues: []string{"chat", "responses"}},
				{FieldName: "Effort", Key: "effort", Label: "Effort", Widget: WidgetText, Visible: true, ReadOnly: false, Order: 30, EnumValues: []string{"none", "low", "medium", "high"}},
			},
		},
		{
			FieldName: "ClaudeHaikuFallbackModelsUI",
			Key:       "claude_haiku_fallback_models_ui",
			Label:     "Haiku Fallbacks",
			Widget:    WidgetArray,
			Visible:   true,
			ReadOnly:  false,
			Order:     67,
			Description: "Ordered replacements for claude-haiku-*.",
			ElementType: reflect.TypeOf(appsettings.HaikuFallbackModel{}),
			ElementSpec: []FieldSpec{
				{FieldName: "Model", Key: "model", Label: "Model", Widget: WidgetText, Visible: true, ReadOnly: false, Order: 10, Placeholder: "gpt-5-mini"},
			},
		},
	}
	return specs, nil
}

func EncodeSettingsToForm(settings *appsettings.Settings, specs []FieldSpec) (SettingsForm, error) {
	if settings == nil {
		defaults := appsettings.DefaultSettings()
		settings = &defaults
	}
	form := SettingsForm{
		ScalarValues:      make(map[string]string),
		KeyValueValues:    make(map[string][]HeaderKV),
		ObjectArrayValues: make(map[string][]map[string]string),
	}

	value := reflect.ValueOf(*settings)
	for i := range specs {
		spec := specs[i]
		field := value.FieldByName(spec.FieldName)
		if !field.IsValid() {
			return SettingsForm{}, fmt.Errorf("%w: %s", errFieldNotFound, spec.FieldName)
		}
		switch spec.Widget {
		case WidgetText, WidgetURL:
			form.ScalarValues[spec.Key] = field.String()
		case WidgetInt:
			form.ScalarValues[spec.Key] = strconv.Itoa(int(field.Int()))
		case WidgetBool:
			form.ScalarValues[spec.Key] = strconv.FormatBool(field.Bool())
		case WidgetDuration:
			durationValue, ok := field.Interface().(Duration)
			if !ok {
				return SettingsForm{}, fmt.Errorf("%w: %s", errInvalidWidgetType, spec.Widget)
			}
			form.ScalarValues[spec.Key] = durationValue.String()
		case WidgetKeyValue:
			rows, rowsErr := encodeMapRows(field)
			if rowsErr != nil {
				return SettingsForm{}, fmt.Errorf("field %s: %w", spec.Key, rowsErr)
			}
			form.KeyValueValues[spec.Key] = rows
		case WidgetArray:
			rows, rowsErr := encodeObjectArrayRows(field, spec.ElementSpec)
			if rowsErr != nil {
				return SettingsForm{}, fmt.Errorf("field %s: %w", spec.Key, rowsErr)
			}
			form.ObjectArrayValues[spec.Key] = rows
		default:
			return SettingsForm{}, fmt.Errorf("%w: %s", errInvalidWidget, spec.Widget)
		}
	}

	return form, nil
}

func DecodeFormToSettings(base *appsettings.Settings, specs []FieldSpec, form SettingsForm) (appsettings.Settings, error) {
	if base == nil {
		defaults := appsettings.DefaultSettings()
		base = &defaults
	}
	out := *base
	encodedBase, err := EncodeSettingsToForm(base, specs)
	if err != nil {
		return appsettings.Settings{}, fmt.Errorf("encode base settings: %w", err)
	}

	value := reflect.ValueOf(&out).Elem()
	for i := range specs {
		spec := &specs[i]
		field := value.FieldByName(spec.FieldName)
		if !field.IsValid() {
			return appsettings.Settings{}, fmt.Errorf("%w: %s", errFieldNotFound, spec.FieldName)
		}

		if spec.ReadOnly {
			if readonlyChanged(spec, encodedBase, form) {
				return appsettings.Settings{}, fmt.Errorf("%w: %s", errReadonlyModified, spec.Key)
			}
			continue
		}
		if !spec.Visible {
			continue
		}

		switch spec.Widget {
		case WidgetText:
			raw := form.ScalarValues[spec.Key]
			if err := validateEnum(raw, spec.EnumValues); err != nil {
				return appsettings.Settings{}, fmt.Errorf("field %s: %w", spec.Key, err)
			}
			field.SetString(raw)
		case WidgetURL:
			rawURL := strings.TrimSpace(form.ScalarValues[spec.Key])
			if err := validateURL(rawURL); err != nil {
				return appsettings.Settings{}, fmt.Errorf("field %s: %w", spec.Key, err)
			}
			if err := validateEnum(rawURL, spec.EnumValues); err != nil {
				return appsettings.Settings{}, fmt.Errorf("field %s: %w", spec.Key, err)
			}
			field.SetString(rawURL)
		case WidgetInt:
			parsedInt, parseErr := parseIntValue(form.ScalarValues[spec.Key], spec.Min, spec.Max, spec.EmptyZero)
			if parseErr != nil {
				return appsettings.Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.SetInt(int64(parsedInt))
		case WidgetBool:
			parsedBool, parseErr := parseBoolValue(form.ScalarValues[spec.Key])
			if parseErr != nil {
				return appsettings.Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.SetBool(parsedBool)
		case WidgetDuration:
			durationValue, parseErr := parseDurationValue(form.ScalarValues[spec.Key])
			if parseErr != nil {
				return appsettings.Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.Set(reflect.ValueOf(durationValue))
		case WidgetKeyValue:
			decodedMap, parseErr := decodeMapRows(form.KeyValueValues[spec.Key], field.Type())
			if parseErr != nil {
				return appsettings.Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.Set(decodedMap)
		case WidgetArray:
			decodedArray, parseErr := decodeObjectArrayRows(form.ObjectArrayValues[spec.Key], field.Type(), spec.ElementSpec)
			if parseErr != nil {
				return appsettings.Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.Set(decodedArray)
		default:
			return appsettings.Settings{}, fmt.Errorf("%w: %s", errInvalidWidget, spec.Widget)
		}
	}

	finalSettings := appsettings.FromRuntimeConfig(appsettings.ToRuntimeConfig(out))
	if err := finalSettings.SyncStorageFieldsFromView(); err != nil {
		return appsettings.Settings{}, fmt.Errorf("sync settings shadow fields: %w", err)
	}
	return finalSettings, nil
}

func readonlyChanged(spec *FieldSpec, base, current SettingsForm) bool {
	if spec == nil {
		return false
	}
	switch spec.Widget {
	case WidgetText, WidgetURL, WidgetInt, WidgetBool, WidgetDuration:
		return base.ScalarValues[spec.Key] != current.ScalarValues[spec.Key]
	case WidgetKeyValue:
		return !reflect.DeepEqual(base.KeyValueValues[spec.Key], current.KeyValueValues[spec.Key])
	case WidgetArray:
		return !reflect.DeepEqual(base.ObjectArrayValues[spec.Key], current.ObjectArrayValues[spec.Key])
	default:
		return false
	}
}

func validateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %s", errInvalidURL, raw)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: %s", errInvalidURL, raw)
	}
	return nil
}

func validateMinMax(value int, minRaw, maxRaw string) error {
	if minRaw != "" {
		minValue, err := strconv.Atoi(minRaw)
		if err != nil {
			return fmt.Errorf("%w: min=%s", errInvalidMinMax, minRaw)
		}
		if value < minValue {
			return fmt.Errorf("%w: below min %d", errInvalidInt, minValue)
		}
	}
	if maxRaw != "" {
		maxValue, err := strconv.Atoi(maxRaw)
		if err != nil {
			return fmt.Errorf("%w: max=%s", errInvalidMinMax, maxRaw)
		}
		if value > maxValue {
			return fmt.Errorf("%w: above max %d", errInvalidInt, maxValue)
		}
	}
	return nil
}

func parseIntValue(raw, minRaw, maxRaw string, emptyZero bool) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		if emptyZero {
			return 0, nil
		}
		return 0, errInvalidInt
	}
	parsedInt, parseErr := strconv.Atoi(trimmed)
	if parseErr != nil {
		return 0, errInvalidInt
	}
	if err := validateMinMax(parsedInt, minRaw, maxRaw); err != nil {
		return 0, err
	}
	return parsedInt, nil
}

func parseDurationValue(raw string) (Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Duration{}, ErrDurationEmpty
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return Duration{}, fmt.Errorf("%w: %s", ErrInvalidDuration, trimmed)
	}
	if parsed < 0 {
		return Duration{}, ErrDurationNegative
	}
	if parsed%time.Second != 0 {
		return Duration{}, fmt.Errorf("%w: %s", ErrDurationWholeSeconds, trimmed)
	}
	return NewDuration(parsed), nil
}

func parseBoolValue(raw string) (bool, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.EqualFold(trimmed, "true") {
		return true, nil
	}
	if strings.EqualFold(trimmed, "false") {
		return false, nil
	}
	return false, fmt.Errorf("%w: %s", errInvalidBool, trimmed)
}

func encodeMapRows(field reflect.Value) ([]HeaderKV, error) {
	if field.Kind() != reflect.Map || field.Type().Key().Kind() != reflect.String {
		return nil, fmt.Errorf("%w: expected map with string key", errInvalidWidgetType)
	}
	if field.Len() == 0 {
		return nil, nil
	}

	keys := field.MapKeys()
	sortedKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		sortedKeys = append(sortedKeys, key.String())
	}
	sort.Strings(sortedKeys)

	rows := make([]HeaderKV, 0, len(sortedKeys))
	for _, key := range sortedKeys {
		value := field.MapIndex(reflect.ValueOf(key))
		encodedValue, err := encodeMapValue(value)
		if err != nil {
			return nil, err
		}
		rows = append(rows, HeaderKV{Key: key, Value: encodedValue})
	}
	return rows, nil
}

func encodeMapValue(value reflect.Value) (string, error) {
	if !value.IsValid() {
		return "", nil
	}
	if value.Kind() == reflect.String {
		return value.String(), nil
	}
	raw, err := json.Marshal(value.Interface())
	if err != nil {
		return "", fmt.Errorf("encode map value: %w", err)
	}
	return string(raw), nil
}

func decodeMapRows(rows []HeaderKV, mapType reflect.Type) (reflect.Value, error) {
	if mapType.Kind() != reflect.Map || mapType.Key().Kind() != reflect.String {
		return reflect.Value{}, fmt.Errorf("%w: expected map[string]T", errInvalidWidgetType)
	}
	decoded := reflect.MakeMapWithSize(mapType, len(rows))
	if len(rows) == 0 {
		return decoded, nil
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.Key)
		if key == "" {
			return reflect.Value{}, errEmptyHeaderKey
		}
		normalizedKey := strings.ToLower(key)
		if _, exists := seen[normalizedKey]; exists {
			return reflect.Value{}, fmt.Errorf("%w: %s", errDuplicateHeaderKey, key)
		}
		seen[normalizedKey] = struct{}{}
		value, err := decodeMapValue(strings.TrimSpace(row.Value), mapType.Elem())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("key %s: %w", key, err)
		}
		decoded.SetMapIndex(reflect.ValueOf(key), value)
	}
	return decoded, nil
}

func decodeMapValue(raw string, targetType reflect.Type) (reflect.Value, error) {
	if targetType.Kind() == reflect.String {
		return reflect.ValueOf(raw).Convert(targetType), nil
	}
	ptr := reflect.New(targetType)
	if err := json.Unmarshal([]byte(raw), ptr.Interface()); err != nil {
		return reflect.Value{}, fmt.Errorf("decode map value json: %w", err)
	}
	return ptr.Elem(), nil
}

func encodeObjectArrayRows(field reflect.Value, elementSpecs []FieldSpec) ([]map[string]string, error) {
	if field.Kind() != reflect.Slice {
		return nil, fmt.Errorf("%w: expected slice", errInvalidWidgetType)
	}
	rows := make([]map[string]string, 0, field.Len())
	for i := 0; i < field.Len(); i++ {
		item := field.Index(i)
		if item.Kind() == reflect.Pointer {
			item = item.Elem()
		}
		if item.Kind() != reflect.Struct {
			return nil, fmt.Errorf("%w: expected struct element", errInvalidWidgetType)
		}
		row := make(map[string]string, len(elementSpecs))
		for _, elementSpec := range elementSpecs {
			fieldValue := item.FieldByName(elementSpec.FieldName)
			encoded, err := encodeScalarFieldValue(fieldValue, &elementSpec)
			if err != nil {
				return nil, err
			}
			row[elementSpec.Key] = encoded
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func decodeObjectArrayRows(rows []map[string]string, targetType reflect.Type, elementSpecs []FieldSpec) (reflect.Value, error) {
	if targetType.Kind() != reflect.Slice {
		return reflect.Value{}, fmt.Errorf("%w: expected slice", errInvalidWidgetType)
	}
	result := reflect.MakeSlice(targetType, 0, len(rows))
	elemType := targetType.Elem()
	isPointerElem := elemType.Kind() == reflect.Pointer
	structType := elemType
	if isPointerElem {
		structType = elemType.Elem()
	}
	for _, row := range rows {
		item := reflect.New(structType).Elem()
		for _, elementSpec := range elementSpecs {
			field := item.FieldByName(elementSpec.FieldName)
			if !field.IsValid() || !field.CanSet() {
				return reflect.Value{}, fmt.Errorf("%w: %s", errFieldNotFound, elementSpec.FieldName)
			}
			if elementSpec.ReadOnly {
				continue
			}
			raw := row[elementSpec.Key]
			if err := decodeScalarFieldValue(field, &elementSpec, raw); err != nil {
				return reflect.Value{}, err
			}
		}
		if isPointerElem {
			ptr := reflect.New(structType)
			ptr.Elem().Set(item)
			result = reflect.Append(result, ptr)
		} else {
			result = reflect.Append(result, item)
		}
	}
	return result, nil
}

func encodeScalarFieldValue(field reflect.Value, spec *FieldSpec) (string, error) {
	if !field.IsValid() || spec == nil {
		return "", nil
	}
	switch spec.Widget {
	case WidgetText, WidgetURL:
		return field.String(), nil
	case WidgetInt:
		return strconv.Itoa(int(field.Int())), nil
	case WidgetBool:
		return strconv.FormatBool(field.Bool()), nil
	case WidgetDuration:
		durationValue, ok := field.Interface().(Duration)
		if !ok {
			return "", fmt.Errorf("%w: %s", errInvalidWidgetType, spec.Widget)
		}
		return durationValue.String(), nil
	default:
		return "", fmt.Errorf("%w: %s", errInvalidWidget, spec.Widget)
	}
}

func decodeScalarFieldValue(field reflect.Value, spec *FieldSpec, raw string) error {
	if !field.IsValid() || spec == nil {
		return nil
	}
	switch spec.Widget {
	case WidgetText:
		if err := validateEnum(raw, spec.EnumValues); err != nil {
			return err
		}
		field.SetString(raw)
	case WidgetURL:
		trimmed := strings.TrimSpace(raw)
		if err := validateURL(trimmed); err != nil {
			return err
		}
		if err := validateEnum(trimmed, spec.EnumValues); err != nil {
			return err
		}
		field.SetString(trimmed)
	case WidgetInt:
		parsedInt, parseErr := parseIntValue(raw, spec.Min, spec.Max, spec.EmptyZero)
		if parseErr != nil {
			return parseErr
		}
		field.SetInt(int64(parsedInt))
	case WidgetBool:
		parsedBool, parseErr := parseBoolValue(raw)
		if parseErr != nil {
			return parseErr
		}
		field.SetBool(parsedBool)
	case WidgetDuration:
		durationValue, parseErr := parseDurationValue(raw)
		if parseErr != nil {
			return parseErr
		}
		field.Set(reflect.ValueOf(durationValue))
	default:
		return fmt.Errorf("%w: %s", errInvalidWidget, spec.Widget)
	}
	return nil
}

func validateEnum(raw string, enumValues []string) error {
	if len(enumValues) == 0 {
		return nil
	}
	for _, item := range enumValues {
		if raw == item {
			return nil
		}
	}
	return fmt.Errorf("%w: expected one of %s", errInvalidEnum, strings.Join(enumValues, ","))
}
