package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Value is a scalar contract value. Objects, arrays, and null are intentionally
// excluded to prevent ambiguous coercions in policy evaluation.
// MaxExactInteger is the largest integer that can be represented exactly by
// IEEE-754 binary64. v1alpha1 deliberately rejects larger integer literals
// instead of silently rounding policy inputs or DRA attributes.
const MaxExactInteger int64 = 1<<53 - 1

type Value struct {
	kind string
	num  float64
	str  string
	b    bool
}

func NumberValue(v float64) Value { return Value{kind: "number", num: v} }
func StringValue(v string) Value  { return Value{kind: "string", str: v} }
func BoolValue(v bool) Value      { return Value{kind: "bool", b: v} }

func (v Value) Kind() string            { return v.kind }
func (v Value) Number() (float64, bool) { return v.num, v.kind == "number" }
func (v Value) String() (string, bool)  { return v.str, v.kind == "string" }
func (v Value) Bool() (bool, bool)      { return v.b, v.kind == "bool" }

func (v Value) MarshalJSON() ([]byte, error) {
	switch v.kind {
	case "number":
		if math.IsNaN(v.num) || math.IsInf(v.num, 0) {
			return nil, errors.New("non-finite number is not valid JSON")
		}
		return json.Marshal(v.num)
	case "string":
		return json.Marshal(v.str)
	case "bool":
		return json.Marshal(v.b)
	default:
		return nil, errors.New("unset contract value")
	}
}

func (v *Value) UnmarshalJSON(data []byte) error {
	if v == nil {
		return errors.New("nil Value receiver")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return errors.New("empty scalar value")
	}
	switch data[0] {
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*v = StringValue(s)
		return nil
	case 't', 'f':
		var b bool
		if err := json.Unmarshal(data, &b); err != nil {
			return err
		}
		*v = BoolValue(b)
		return nil
	case '{', '[', 'n':
		return errors.New("only number, string, and boolean scalar values are allowed")
	default:
		var n json.Number
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&n); err != nil {
			return fmt.Errorf("decode number: %w", err)
		}
		literal := n.String()
		if !strings.ContainsAny(literal, ".eE") {
			i, intErr := n.Int64()
			if intErr != nil || i > MaxExactInteger || i < -MaxExactInteger {
				return fmt.Errorf("integer %q exceeds the exact v1alpha1 range [%d,%d]", literal, -MaxExactInteger, MaxExactInteger)
			}
		}
		f, err := n.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("invalid finite number %q", literal)
		}
		*v = NumberValue(f)
		return nil
	}
}
