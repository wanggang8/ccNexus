package proxy

import (
	"encoding/json"
	"testing"
)

func TestApplyRequestOverrides_Empty(t *testing.T) {
	body := []byte(`{"model":"gpt-4","stream":true}`)
	result, err := applyRequestOverrides(body, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != string(body) {
		t.Errorf("expected unchanged body, got %s", result)
	}
}

func TestApplyRequestOverrides_AddField(t *testing.T) {
	body := []byte(`{"model":"gpt-4"}`)
	overrides := `{"temperature":0.7}`
	result, err := applyRequestOverrides(body, overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	json.Unmarshal(result, &m)
	if m["temperature"] != 0.7 {
		t.Errorf("expected temperature=0.7, got %v", m["temperature"])
	}
	if m["model"] != "gpt-4" {
		t.Errorf("expected model=gpt-4, got %v", m["model"])
	}
}

func TestApplyRequestOverrides_ReplaceField(t *testing.T) {
	body := []byte(`{"model":"gpt-4","max_tokens":100}`)
	overrides := `{"model":"claude-3","max_tokens":200}`
	result, err := applyRequestOverrides(body, overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	json.Unmarshal(result, &m)
	if m["model"] != "claude-3" {
		t.Errorf("expected model=claude-3, got %v", m["model"])
	}
	if m["max_tokens"] != float64(200) {
		t.Errorf("expected max_tokens=200, got %v", m["max_tokens"])
	}
}

func TestApplyRequestOverrides_DeleteField(t *testing.T) {
	body := []byte(`{"model":"gpt-4","temperature":0.5,"stream":true}`)
	overrides := `{"temperature":null}`
	result, err := applyRequestOverrides(body, overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	json.Unmarshal(result, &m)
	if _, ok := m["temperature"]; ok {
		t.Error("expected temperature to be deleted")
	}
	if m["model"] != "gpt-4" {
		t.Errorf("expected model preserved, got %v", m["model"])
	}
}

func TestApplyRequestOverrides_DeepMerge(t *testing.T) {
	body := []byte(`{"model":"gpt-4","stream_options":{"include_usage":true,"other":"keep"}}`)
	overrides := `{"stream_options":{"include_usage":false}}`
	result, err := applyRequestOverrides(body, overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	json.Unmarshal(result, &m)
	so := m["stream_options"].(map[string]interface{})
	if so["include_usage"] != false {
		t.Errorf("expected include_usage=false, got %v", so["include_usage"])
	}
	if so["other"] != "keep" {
		t.Errorf("expected other=keep preserved, got %v", so["other"])
	}
}

func TestApplyRequestOverrides_DeepMergeDelete(t *testing.T) {
	body := []byte(`{"metadata":{"key1":"val1","key2":"val2"}}`)
	overrides := `{"metadata":{"key1":null,"key3":"val3"}}`
	result, err := applyRequestOverrides(body, overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	json.Unmarshal(result, &m)
	md := m["metadata"].(map[string]interface{})
	if _, ok := md["key1"]; ok {
		t.Error("expected key1 to be deleted in nested object")
	}
	if md["key2"] != "val2" {
		t.Errorf("expected key2=val2 preserved, got %v", md["key2"])
	}
	if md["key3"] != "val3" {
		t.Errorf("expected key3=val3 added, got %v", md["key3"])
	}
}

func TestApplyRequestOverrides_OverrideNestedWithScalar(t *testing.T) {
	body := []byte(`{"options":{"a":1,"b":2}}`)
	overrides := `{"options":"simple"}`
	result, err := applyRequestOverrides(body, overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	json.Unmarshal(result, &m)
	if m["options"] != "simple" {
		t.Errorf("expected options=simple, got %v", m["options"])
	}
}

func TestApplyRequestOverrides_InvalidBodyJSON(t *testing.T) {
	_, err := applyRequestOverrides([]byte("not json"), `{"a":1}`)
	if err == nil {
		t.Error("expected error for invalid body JSON")
	}
}

func TestApplyRequestOverrides_InvalidOverridesJSON(t *testing.T) {
	_, err := applyRequestOverrides([]byte(`{"a":1}`), "not json")
	if err == nil {
		t.Error("expected error for invalid overrides JSON")
	}
}

func TestApplyRequestOverrides_MultipleOperations(t *testing.T) {
	body := []byte(`{"model":"old","keep":"yes","remove":"me","nested":{"a":1}}`)
	overrides := `{"model":"new","remove":null,"add":"field","nested":{"b":2}}`
	result, err := applyRequestOverrides(body, overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	json.Unmarshal(result, &m)
	if m["model"] != "new" {
		t.Errorf("expected model=new, got %v", m["model"])
	}
	if m["keep"] != "yes" {
		t.Errorf("expected keep=yes, got %v", m["keep"])
	}
	if _, ok := m["remove"]; ok {
		t.Error("expected remove to be deleted")
	}
	if m["add"] != "field" {
		t.Errorf("expected add=field, got %v", m["add"])
	}
	nested := m["nested"].(map[string]interface{})
	if nested["a"] != float64(1) {
		t.Errorf("expected nested.a=1 preserved, got %v", nested["a"])
	}
	if nested["b"] != float64(2) {
		t.Errorf("expected nested.b=2 added, got %v", nested["b"])
	}
}
