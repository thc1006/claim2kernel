// Package jsonsafe provides bounded, strict JSON decoding with recursive
// duplicate-key rejection. Go's encoding/json otherwise accepts duplicate
// object keys and silently keeps the last value, which is unsafe for signed
// contracts and policy inputs.
package jsonsafe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	DefaultMaxBytes = 4 << 20
	DefaultMaxDepth = 64
)

// DecodeStrict decodes exactly one JSON value into dst. Unknown fields,
// duplicate object keys, trailing values, excessive depth, and oversized input
// are rejected.
func DecodeStrict(data []byte, dst any, maxBytes int) error {
	if dst == nil {
		return errors.New("destination must not be nil")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if len(data) > maxBytes {
		return fmt.Errorf("JSON input exceeds %d bytes", maxBytes)
	}
	if err := RejectDuplicateKeys(data, DefaultMaxDepth); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode strict JSON: %w", err)
	}
	if err := requireEOF(dec); err != nil {
		return err
	}
	return nil
}

// RejectDuplicateKeys checks an arbitrary JSON value without materializing it.
func RejectDuplicateKeys(data []byte, maxDepth int) error {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := parseValue(dec, 0, maxDepth); err != nil {
		return err
	}
	return requireEOF(dec)
}

func parseValue(dec *json.Decoder, depth, maxDepth int) error {
	if depth > maxDepth {
		return fmt.Errorf("JSON nesting exceeds maximum depth %d", maxDepth)
	}
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("read JSON token: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return fmt.Errorf("read object key: %w", err)
			}
			key, ok := keyTok.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := parseValue(dec, depth+1, maxDepth); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("unterminated JSON object: %w", err)
		}
	case '[':
		for dec.More() {
			if err := parseValue(dec, depth+1, maxDepth); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("unterminated JSON array: %w", err)
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	return nil
}

func requireEOF(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("trailing JSON value is not allowed")
	}
	return fmt.Errorf("trailing JSON data: %w", err)
}
