package gemini

import (
	"reflect"
	"testing"
)

func TestCleanFunctionParametersDropsNonStringEnum(t *testing.T) {
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"allowAsync": map[string]interface{}{"type": "boolean", "enum": []interface{}{true}},
			"mode":       map[string]interface{}{"type": []interface{}{"string", "null"}, "enum": []interface{}{"a", "b", nil}},
			"level":      map[string]interface{}{"type": "integer", "enum": []interface{}{1.0, 2.0}},
		},
	}
	props := CleanFunctionParameters(params).(map[string]interface{})["properties"].(map[string]interface{})

	if _, ok := props["allowAsync"].(map[string]interface{})["enum"]; ok {
		t.Errorf("boolean enum kept: %v", props["allowAsync"])
	}
	if _, ok := props["level"].(map[string]interface{})["enum"]; ok {
		t.Errorf("integer enum kept: %v", props["level"])
	}
	if got := props["mode"].(map[string]interface{})["enum"]; !reflect.DeepEqual(got, []interface{}{"a", "b"}) {
		t.Errorf("string enum = %v, want [a b]", got)
	}
}
