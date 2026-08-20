package jsonsafe

import (
	"strings"
	"testing"
)

type sample struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func TestDecodeStrict(t *testing.T) {
	var got sample
	if err := DecodeStrict([]byte(`{"name":"x","n":2}`), &got, 1024); err != nil {
		t.Fatal(err)
	}
	if got.Name != "x" || got.N != 2 {
		t.Fatalf("unexpected value: %+v", got)
	}
}

func TestRejectDuplicateNestedKey(t *testing.T) {
	var got map[string]any
	err := DecodeStrict([]byte(`{"outer":{"x":1,"x":2}}`), &got, 1024)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-key error, got %v", err)
	}
}

func TestRejectUnknownField(t *testing.T) {
	var got sample
	err := DecodeStrict([]byte(`{"name":"x","n":2,"extra":true}`), &got, 1024)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestRejectTrailingValue(t *testing.T) {
	var got sample
	if err := DecodeStrict([]byte(`{"name":"x","n":2} {}`), &got, 1024); err == nil {
		t.Fatal("expected trailing-value error")
	}
}

func TestRejectDepth(t *testing.T) {
	data := strings.Repeat("[", 70) + strings.Repeat("]", 70)
	if err := RejectDuplicateKeys([]byte(data), 32); err == nil {
		t.Fatal("expected depth error")
	}
}

func FuzzDecodeStrict(f *testing.F) {
	f.Add([]byte(`{"name":"seed","n":1}`))
	f.Add([]byte(`{"x":1,"x":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var dst sample
		_ = DecodeStrict(data, &dst, 1<<20)
	})
}
