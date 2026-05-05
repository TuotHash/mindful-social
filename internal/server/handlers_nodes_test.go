package server

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mindful-social/mindful-social/internal/db"
)

// makeOut builds a synthetic ListEdgesFromNode row.
func makeOut(kind db.EdgeKind, position *int16, toID uuid.UUID, toType db.NodeType, toTitle string) db.ListEdgesFromNodeRow {
	return db.ListEdgesFromNodeRow{
		ID:        uuid.New(),
		Kind:      kind,
		Position:  position,
		CreatedAt: pgtype.Timestamptz{},
		ToID:      toID,
		ToType:    toType,
		ToTitle:   toTitle,
	}
}

// makeIn builds a synthetic ListEdgesToNode row.
func makeIn(kind db.EdgeKind, fromID uuid.UUID, fromType db.NodeType, fromTitle string) db.ListEdgesToNodeRow {
	return db.ListEdgesToNodeRow{
		ID:        uuid.New(),
		Kind:      kind,
		CreatedAt: pgtype.Timestamptz{},
		FromID:    fromID,
		FromType:  fromType,
		FromTitle: fromTitle,
	}
}

func TestDisplayGroups_emptyInput(t *testing.T) {
	got := displayGroups(nil, nil)
	if len(got) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(got))
	}
}

func TestDisplayGroups_outgoingOnly_assignsActiveLabel(t *testing.T) {
	target := uuid.New()
	out := []db.ListEdgesFromNodeRow{makeOut(db.EdgeKindSupports, nil, target, db.NodeTypeReasoning, "R")}
	groups := displayGroups(out, nil)

	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(groups))
	}
	g := groups[0]
	if g.Label != "Supports" {
		t.Fatalf("label = %q, want %q", g.Label, "Supports")
	}
	if len(g.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(g.Rows))
	}
	if !g.Rows[0].Outgoing {
		t.Fatalf("row.Outgoing = false, want true")
	}
}

func TestDisplayGroups_incomingOnly_assignsPassiveLabel(t *testing.T) {
	source := uuid.New()
	in := []db.ListEdgesToNodeRow{makeIn(db.EdgeKindSupports, source, db.NodeTypeReasoning, "R")}
	groups := displayGroups(nil, in)

	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(groups))
	}
	g := groups[0]
	if g.Label != "Supported by" {
		t.Fatalf("label = %q, want %q", g.Label, "Supported by")
	}
	if g.Rows[0].Outgoing {
		t.Fatalf("row.Outgoing = true, want false (incoming)")
	}
}

func TestDisplayGroups_outgoingAndIncoming_sameKind_yieldsTwoGroups(t *testing.T) {
	out := []db.ListEdgesFromNodeRow{makeOut(db.EdgeKindRefines, nil, uuid.New(), db.NodeTypeTopic, "T")}
	in := []db.ListEdgesToNodeRow{makeIn(db.EdgeKindRefines, uuid.New(), db.NodeTypeView, "V")}
	groups := displayGroups(out, in)

	if len(groups) != 2 {
		t.Fatalf("want 2 groups (active + passive), got %d", len(groups))
	}
	if groups[0].Label != "Refines" || groups[1].Label != "Refined by" {
		t.Fatalf("labels = [%q, %q], want [%q, %q]", groups[0].Label, groups[1].Label, "Refines", "Refined by")
	}
}

func TestDisplayGroups_relatesTo_mergesIntoOneBucket(t *testing.T) {
	out := []db.ListEdgesFromNodeRow{makeOut(db.EdgeKindRelatesTo, nil, uuid.New(), db.NodeTypeView, "A")}
	in := []db.ListEdgesToNodeRow{makeIn(db.EdgeKindRelatesTo, uuid.New(), db.NodeTypeView, "B")}
	groups := displayGroups(out, in)

	if len(groups) != 1 {
		t.Fatalf("want 1 merged group, got %d", len(groups))
	}
	if groups[0].Label != "Relates to" {
		t.Fatalf("label = %q, want %q", groups[0].Label, "Relates to")
	}
	if len(groups[0].Rows) != 2 {
		t.Fatalf("merged rows = %d, want 2", len(groups[0].Rows))
	}
	// Outgoing first, incoming second (preserves order from displayGroups).
	if !groups[0].Rows[0].Outgoing || groups[0].Rows[1].Outgoing {
		t.Fatalf("row directions = [%v, %v], want [true, false]", groups[0].Rows[0].Outgoing, groups[0].Rows[1].Outgoing)
	}
}

func TestDisplayGroups_featuredOutgoing_excludedFromLegend(t *testing.T) {
	pos := int16(1)
	out := []db.ListEdgesFromNodeRow{
		makeOut(db.EdgeKindSupports, &pos, uuid.New(), db.NodeTypeReasoning, "Featured"),
		makeOut(db.EdgeKindSupports, nil, uuid.New(), db.NodeTypeReasoning, "Legend"),
	}
	groups := displayGroups(out, nil)

	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(groups))
	}
	if len(groups[0].Rows) != 1 {
		t.Fatalf("rows = %d, want 1 (the featured row should be excluded)", len(groups[0].Rows))
	}
	if groups[0].Rows[0].Title != "Legend" {
		t.Fatalf("kept the wrong row: got %q", groups[0].Rows[0].Title)
	}
}

func TestDisplayGroups_kindOrderingIsCanonical(t *testing.T) {
	out := []db.ListEdgesFromNodeRow{
		makeOut(db.EdgeKindRelatesTo, nil, uuid.New(), db.NodeTypeView, "rel"),
		makeOut(db.EdgeKindCites, nil, uuid.New(), db.NodeTypeFact, "cite"),
		makeOut(db.EdgeKindSupports, nil, uuid.New(), db.NodeTypeReasoning, "sup"),
		makeOut(db.EdgeKindRefines, nil, uuid.New(), db.NodeTypeTopic, "ref"),
		makeOut(db.EdgeKindOpposes, nil, uuid.New(), db.NodeTypeView, "opp"),
	}
	groups := displayGroups(out, nil)

	wantOrder := []db.EdgeKind{
		db.EdgeKindSupports, db.EdgeKindOpposes, db.EdgeKindRefines, db.EdgeKindCites, db.EdgeKindRelatesTo,
	}
	if len(groups) != len(wantOrder) {
		t.Fatalf("want %d groups, got %d", len(wantOrder), len(groups))
	}
	for i, g := range groups {
		if g.Kind != wantOrder[i] {
			t.Fatalf("group %d: kind = %q, want %q", i, g.Kind, wantOrder[i])
		}
	}
}

