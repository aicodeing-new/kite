package cluster

import (
	"reflect"
	"testing"
)

func TestNormalizeGPUResourceRules(t *testing.T) {
	got, err := normalizeGPUResourceRules([]string{
		" huawei.com/Ascend910 ",
		"!example.com/gpu.memory",
		"huawei.com/Ascend910",
		"",
	})
	if err != nil {
		t.Fatalf("normalizeGPUResourceRules() error = %v", err)
	}
	want := []string{"huawei.com/Ascend910", "!example.com/gpu.memory"}
	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("normalizeGPUResourceRules() = %#v, want %#v", got, want)
	}
}

func TestNormalizeGPUResourceRulesRejectsInvalidName(t *testing.T) {
	if _, err := normalizeGPUResourceRules([]string{"not a resource"}); err == nil {
		t.Fatal("normalizeGPUResourceRules() unexpectedly accepted an invalid resource name")
	}
}
