package devices

import (
	"fmt"
	"reflect"
	"time"
)

// timeType marks time.Time values, which serialise as RFC3339 strings.
var timeType = reflect.TypeOf(time.Time{})

// InfluxField is one serialised field of a DeviceData struct.
type InfluxField struct {
	Key   string
	Value any
}

// InfluxFields flattens a DeviceData struct into influx line protocol /
// JSON fields (openant issue #117: the struct tags alone are not enough
// because several fields are pointers, arrays or nested structs).
//
// The conversion rules are:
//   - the field key comes from the `influx:"name"` tag; `influx:"-"`
//     fields are skipped;
//   - signed/unsigned integers (including the profile enum types) become
//     int64, floats become float64, strings and bools pass through;
//   - nil pointers are skipped, non-nil pointers are dereferenced;
//   - arrays and slices become one field per element, keyed `<name>_0`,
//     `<name>_1`, ... (nil pointers inside are skipped);
//   - nested structs are flattened as `<name>_<subfield>` (this covers
//     the [15]BatteryData battery array of common pages).
//
// The returned order follows the struct field order, which keeps the
// output stable across writes.
func InfluxFields(data DeviceData) []InfluxField {
	v := reflect.ValueOf(data)
	return appendFields(nil, "", v)
}

// appendFields walks a struct (or any value) with a key prefix.
func appendFields(out []InfluxField, prefix string, v reflect.Value) []InfluxField {
	v = indirect(v)
	if !v.IsValid() {
		return out
	}
	if v.Kind() == reflect.Struct {
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			key := f.Tag.Get("influx")
			if key == "-" {
				continue
			}
			if key == "" {
				key = f.Name
			}
			out = appendValue(out, joinKey(prefix, key), v.Field(i))
		}
		return out
	}
	// A bare scalar reached through appendValue already handled kinds;
	// a struct-less direct call falls back to a scalar write.
	if fv, ok := scalarValue(v); ok {
		return append(out, InfluxField{Key: prefix, Value: fv})
	}
	return out
}

// appendValue appends one tagged field, expanding pointers, collections
// and nested structs.
func appendValue(out []InfluxField, key string, v reflect.Value) []InfluxField {
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return out // skip nil pointers (#117: calculated_speed etc.)
		}
		return appendValue(out, key, v.Elem())
	}
	switch v.Kind() {
	case reflect.Array, reflect.Slice:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return out
		}
		for i := 0; i < v.Len(); i++ {
			elem := v.Index(i)
			if indirect(elem).Kind() == reflect.Struct {
				out = appendFields(out, joinKey(key, fmt.Sprint(i)), elem)
			} else {
				out = appendValue(out, joinKey(key, fmt.Sprint(i)), elem)
			}
		}
		return out
	case reflect.Struct:
		if v.Type() == timeType {
			if val, ok := scalarValue(v); ok {
				return append(out, InfluxField{Key: key, Value: val})
			}
		}
		return appendFields(out, key, v)
	default:
		if val, ok := scalarValue(v); ok {
			return append(out, InfluxField{Key: key, Value: val})
		}
		return out // unsupported kinds (complex, chan, ...) are skipped
	}
}

// scalarValue converts scalar kinds to influx field values; ok is false
// for kinds that have no representation.
func scalarValue(v reflect.Value) (any, bool) {
	if v.Type() == timeType {
		return v.Interface().(time.Time).Format(time.RFC3339Nano), true
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint()), true // influx has no unsigned ints
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	case reflect.Bool:
		return v.Bool(), true
	case reflect.String:
		return v.String(), true
	}
	return nil, false
}

// indirect dereferences pointers and interfaces.
func indirect(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func joinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "_" + key
}

// InfluxMeasurement returns the influx measurement name of a DeviceData
// (the type name, matching openant's to_influx_json measurement field).
func InfluxMeasurement(data DeviceData) string {
	return data.DataName()
}
