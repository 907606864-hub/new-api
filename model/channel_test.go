package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
)

func TestChannelInfoScanAcceptsStringValue(t *testing.T) {
	var info ChannelInfo
	err := info.Scan(`{"is_multi_key":true,"multi_key_size":2,"multi_key_status_list":{"1":2},"multi_key_disabled_reason":{"1":"quota exceeded"},"multi_key_disabled_time":{"1":123},"multi_key_polling_index":1,"multi_key_mode":"polling"}`)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if !info.IsMultiKey {
		t.Fatal("expected multi-key channel info")
	}
	if info.MultiKeySize != 2 {
		t.Fatalf("expected multi-key size 2, got %d", info.MultiKeySize)
	}
	if got := info.MultiKeyStatusList[1]; got != 2 {
		t.Fatalf("expected status 2 for key 1, got %d", got)
	}
	if got := info.MultiKeyDisabledReason[1]; got != "quota exceeded" {
		t.Fatalf("expected disabled reason to be decoded, got %q", got)
	}
	if got := info.MultiKeyDisabledTime[1]; got != 123 {
		t.Fatalf("expected disabled time 123, got %d", got)
	}
	if info.MultiKeyPollingIndex != 1 {
		t.Fatalf("expected polling index 1, got %d", info.MultiKeyPollingIndex)
	}
	if info.MultiKeyMode != constant.MultiKeyModePolling {
		t.Fatalf("expected polling mode, got %q", info.MultiKeyMode)
	}
}

func TestChannelInfoScanAcceptsByteSliceValue(t *testing.T) {
	var info ChannelInfo
	err := info.Scan([]byte(`{"is_multi_key":false,"multi_key_size":0,"multi_key_status_list":null,"multi_key_polling_index":0,"multi_key_mode":"random"}`))
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if info.IsMultiKey {
		t.Fatal("expected single-key channel info")
	}
	if info.MultiKeyMode != constant.MultiKeyModeRandom {
		t.Fatalf("expected random mode, got %q", info.MultiKeyMode)
	}
}

func TestChannelInfoScanDefaultsEmptyValues(t *testing.T) {
	for _, value := range []interface{}{nil, "", []byte("  ")} {
		info := ChannelInfo{IsMultiKey: true, MultiKeySize: 3}
		if err := info.Scan(value); err != nil {
			t.Fatalf("Scan(%v) returned error: %v", value, err)
		}
		if info.IsMultiKey || info.MultiKeySize != 0 || info.MultiKeyPollingIndex != 0 || info.MultiKeyMode != "" {
			t.Fatalf("expected zero-value channel info after Scan(%v), got %+v", value, info)
		}
	}
}
