package event

import (
	"testing"

	"github.com/google/uuid"
)

func TestMakeEvent_Success(t *testing.T) {
	p := PayloadAddURL{ShardID: 1, ID: 123, URL: "https://example.com"}

	ev, err := MakeEvent(p, nil)
	if err != nil {
		t.Fatalf("Failed to make event: %v", err)
	}

	if ev.GetType() != EvAddURL {
		t.Errorf("Expected type %v, got %v", EvAddURL, ev.GetType())
	}

	if ev.GetID() == uuid.Nil {
		t.Error("Event ID should not be nil")
	}

	var dest PayloadAddURL
	if err := ev.GetPayload(&dest); err != nil {
		t.Fatalf("Failed to get payload: %v", err)
	}

	if dest.URL != p.URL || dest.ID != p.ID {
		t.Errorf("Payload content mismatch. Got %+v, want %+v", dest, p)
	}
}

func TestEvent_Chain(t *testing.T) {
	// Создаем родительское событие
	p1 := PayloadAddURL{ID: 1}
	ev1, _ := MakeEvent(p1, nil)

	// Создаем дочернее
	p2 := PayloadBatchItem{CorrelationID: "tx-1"}
	ev2, err := MakeEvent(p2, ev1)
	if err != nil {
		t.Fatalf("Failed to make child event: %v", err)
	}

	parents := ev2.GetParents()
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	if parents[0] != ev1.GetID() {
		t.Errorf("Wrong parent ID. Got %v, want %v", parents[0], ev1.GetID())
	}
}

func TestEvent_Serialization(t *testing.T) {
	p := PayloadAddURL{ShardID: 2, URL: "http://test.io"}
	ev, _ := MakeEvent(p, nil)

	data, err := ev.Serialize()
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.GetID() != ev.GetID() {
		t.Error("Parsed event ID mismatch")
	}

	var pDest PayloadAddURL
	if err := parsed.GetPayload(&pDest); err != nil {
		t.Errorf("Could not extract payload from parsed event: %v", err)
	}
}

func TestGetPayload_Validation(t *testing.T) {
	p := PayloadAddURL{ID: 55}
	ev, _ := MakeEvent(p, nil)

	t.Run("Non-pointer destination", func(t *testing.T) {
		var dest PayloadAddURL
		err := ev.GetPayload(dest) // Ошибка: не указатель
		if err == nil {
			t.Error("Expected error when passing value instead of pointer")
		}
	})

	t.Run("Type mismatch", func(t *testing.T) {
		var dest PayloadBatchItem // Ошибка: не тот тип payload
		err := ev.GetPayload(&dest)
		if err == nil {
			t.Error("Expected error due to EvType mismatch")
		}
	})
}

func TestParse_Errors(t *testing.T) {
	t.Run("Missing ID", func(t *testing.T) {
		invalidJSON := []byte(`{"type": 1, "payload": {}}`)
		_, err := Parse(invalidJSON)
		if err == nil || err.Error() != "event ID is missing" {
			t.Errorf("Expected 'event ID is missing' error, got: %v", err)
		}
	})

	t.Run("Bad JSON", func(t *testing.T) {
		_, err := Parse([]byte(`{not-a-json}`))
		if err == nil {
			t.Error("Expected error on malformed JSON")
		}
	})
}

func TestEvType_String(t *testing.T) {
	tests := []struct {
		val  EvType
		want string
	}{
		{EvAddURL, "EvAddURL"},
		{EvBatchItem, "EvBatchItem"},
		{EvUnknown, "EvUnknown"},
		{EvType(100), "EvType(100)"},
	}

	for _, tt := range tests {
		if tt.val.String() != tt.want {
			t.Errorf("String() for %d: got %s, want %s", tt.val, tt.val.String(), tt.want)
		}
	}
}
