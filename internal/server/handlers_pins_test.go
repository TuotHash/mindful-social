package server

import (
	"testing"

	"github.com/mindful-social/mindful-social/internal/db"
)

func TestParsePinKind(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		nodeType db.NodeType
		wantKind db.PinKind
		wantErr  bool
	}{
		{"empty raw is invalid", "", db.NodeTypeView, "", true},
		{"unknown raw is invalid", "loves", db.NodeTypeView, "", true},
		{"supports on view", "supports", db.NodeTypeView, db.PinKindSupports, false},
		{"opposes on view", "opposes", db.NodeTypeView, db.PinKindOpposes, false},
		{"featured on view", "featured", db.NodeTypeView, db.PinKindFeatured, false},
		{"featured on topic", "featured", db.NodeTypeTopic, db.PinKindFeatured, false},
		{"featured on reasoning", "featured", db.NodeTypeReasoning, db.PinKindFeatured, false},
		{"featured on fact", "featured", db.NodeTypeFact, db.PinKindFeatured, false},
		{"supports on topic rejected", "supports", db.NodeTypeTopic, "", true},
		{"opposes on reasoning rejected", "opposes", db.NodeTypeReasoning, "", true},
		{"supports on fact rejected", "supports", db.NodeTypeFact, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotKind, gotErr := parsePinKind(c.raw, c.nodeType)
			if (gotErr != "") != c.wantErr {
				t.Fatalf("err mismatch: got %q, wantErr=%v", gotErr, c.wantErr)
			}
			if gotKind != c.wantKind {
				t.Fatalf("kind mismatch: got %q, want %q", gotKind, c.wantKind)
			}
		})
	}
}
