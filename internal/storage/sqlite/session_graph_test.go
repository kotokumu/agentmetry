package sqlite

import "testing"

func TestSessionGraphBuildsNestedGroupsAndDeduplicatesEdges(t *testing.T) {
	parent := sessionRef{sourceID: "codex", sessionID: "parent"}
	child := sessionRef{sourceID: "codex", sessionID: "child"}
	grandchild := sessionRef{sourceID: "codex", sessionID: "grandchild"}
	graph := newSessionGraph(
		map[sessionRef]struct{}{parent: {}, child: {}, grandchild: {}},
		map[sessionRef]map[sessionRef]struct{}{
			child:      {parent: {}},
			grandchild: {child: {}},
		},
	)

	if got := graph.root(grandchild); got != parent {
		t.Fatalf("grandchild root = %#v, want %#v", got, parent)
	}
	if got := graph.members(child); len(got) != 3 || got[0] != child || got[1] != grandchild || got[2] != parent {
		t.Fatalf("nested group members = %#v", got)
	}
}

func TestSessionGraphLeavesAmbiguousAndCyclicChildrenIndependent(t *testing.T) {
	parentOne := sessionRef{sourceID: "codex", sessionID: "parent-1"}
	parentTwo := sessionRef{sourceID: "codex", sessionID: "parent-2"}
	ambiguous := sessionRef{sourceID: "codex", sessionID: "ambiguous"}
	cycleOne := sessionRef{sourceID: "codex", sessionID: "cycle-1"}
	cycleTwo := sessionRef{sourceID: "codex", sessionID: "cycle-2"}
	nodes := map[sessionRef]struct{}{parentOne: {}, parentTwo: {}, ambiguous: {}, cycleOne: {}, cycleTwo: {}}
	graph := newSessionGraph(nodes, map[sessionRef]map[sessionRef]struct{}{
		ambiguous: {parentOne: {}, parentTwo: {}},
		cycleOne:  {cycleTwo: {}},
		cycleTwo:  {cycleOne: {}},
	})

	for _, ref := range []sessionRef{ambiguous, cycleOne, cycleTwo} {
		if got := graph.root(ref); got != ref {
			t.Fatalf("unsafe relationship grouped %#v under %#v", ref, got)
		}
		if got := graph.members(ref); len(got) != 1 || got[0] != ref {
			t.Fatalf("unsafe relationship members for %#v = %#v", ref, got)
		}
	}
}

func TestSessionGraphNamespacesIdenticalConversationIDsBySource(t *testing.T) {
	codexParent := sessionRef{sourceID: "codex", sessionID: "parent"}
	codexChild := sessionRef{sourceID: "codex", sessionID: "shared-child"}
	claudeChild := sessionRef{sourceID: "claude", sessionID: "shared-child"}
	graph := newSessionGraph(
		map[sessionRef]struct{}{codexParent: {}, codexChild: {}, claudeChild: {}},
		map[sessionRef]map[sessionRef]struct{}{codexChild: {codexParent: {}}},
	)

	if graph.root(codexChild) != codexParent {
		t.Fatalf("codex child was not grouped")
	}
	if graph.root(claudeChild) != claudeChild {
		t.Fatalf("cross-source child was grouped")
	}
}
