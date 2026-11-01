package chess

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPositionKeyFixture(t *testing.T) {
	data, err := os.ReadFile("../../tests/assets/position_keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name string `json:"name"`
		FEN  string `json:"fen"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := PositionKey(c.FEN); got != c.Key {
				t.Errorf("PositionKey(%q) = %q, want %q", c.FEN, got, c.Key)
			}
		})
	}
}
