package tui

import (
	"strings"
	"testing"
)

func TestResolveCommandAcceptsNamesAndAliases(t *testing.T) {
	registry := map[string]func() View{
		"projects": func() View { return fakeView{title: "Projects"} },
		"proj":     func() View { return fakeView{title: "Projects"} },
		"p":        func() View { return fakeView{title: "Projects"} },
	}
	for _, name := range []string{"projects", "proj", "p", "  projects  ", "PROJECTS"} {
		view, ok := resolveCommand(name, registry)
		if !ok {
			t.Errorf("resolveCommand(%q) failed", name)
			continue
		}
		if view.Title() != "Projects" {
			t.Errorf("resolveCommand(%q) = %q", name, view.Title())
		}
	}
	if _, ok := resolveCommand("nope", registry); ok {
		t.Error("unknown command must not resolve")
	}
}

func TestCommandPromptReplacesTheStack(t *testing.T) {
	m := newTestModel()
	m.registry = map[string]func() View{
		"child": func() View { return fakeView{title: "Child", leaf: true} },
	}
	next, _ := m.Update(key("enter")) // depth 2
	m = next.(Model)
	next, _ = m.Update(key(":"))
	m = next.(Model)
	if m.mode != modeCommand {
		t.Fatal("':' must open the command prompt")
	}
	for _, r := range "child" {
		next, _ = m.Update(key(string(r)))
		m = next.(Model)
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if m.mode != modeNormal {
		t.Error("submitting the prompt must return to normal mode")
	}
	if len(m.stack) != 1 {
		t.Errorf("a command replaces the whole stack, depth = %d, want 1", len(m.stack))
	}
	if m.current().view.Title() != "Child" {
		t.Errorf("view = %q, want Child", m.current().view.Title())
	}
}

func TestUnknownCommandLeavesStackUntouched(t *testing.T) {
	m := newTestModel()
	m.registry = map[string]func() View{}
	next, _ := m.Update(key(":"))
	m = next.(Model)
	for _, r := range "zzz" {
		next, _ = m.Update(key(string(r)))
		m = next.(Model)
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if len(m.stack) != 1 || m.current().view.Title() != "Root" {
		t.Error("an unknown command must not change the stack")
	}
	if m.commandErr == "" {
		t.Error("an unknown command must report an inline error")
	}
}

func TestEscCancelsPromptWithoutPopping(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("enter")) // depth 2
	m = next.(Model)
	next, _ = m.Update(key(":"))
	m = next.(Model)
	next, _ = m.Update(key("esc"))
	m = next.(Model)
	if m.mode != modeNormal {
		t.Error("esc must close the prompt")
	}
	if len(m.stack) != 2 {
		t.Errorf("esc closing a prompt must not also pop the stack, depth = %d", len(m.stack))
	}
}

func TestFilterPromptFiltersTheTable(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("/"))
	m = next.(Model)
	if m.mode != modeFilter {
		t.Fatal("'/' must open the filter prompt")
	}
	for _, r := range "alp" {
		next, _ = m.Update(key(string(r)))
		m = next.(Model)
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if m.current().table.VisibleCount() != 1 {
		t.Errorf("visible = %d, want 1", m.current().table.VisibleCount())
	}
	if m.current().table.TotalCount() != 2 {
		t.Errorf("total = %d, want 2", m.current().table.TotalCount())
	}
}

func TestPromptKeysAreNotTreatedAsGlobalKeys(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("/"))
	m = next.(Model)
	// "2" would normally set the claude tool filter.
	next, _ = m.Update(key("2"))
	m = next.(Model)
	if m.scope.Tool != "" {
		t.Error("keys typed into a prompt must not fire global bindings")
	}
	if !strings.Contains(m.input, "2") {
		t.Errorf("input = %q, want it to contain the typed character", m.input)
	}
}
