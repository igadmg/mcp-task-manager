package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

// newFieldsBacklog builds a real on-disk backlog with two records that carry
// hand-written fields, so a test can run a service flow over them and read
// the files back.
func newFieldsBacklog(t *testing.T) (*task.Service, *MarkdownStorage, string) {
	t.Helper()

	dir := t.TempDir()
	st := NewMarkdownStorage(dir)
	writeRecord(t, dir, "7", recordWithFields)
	writeRecord(t, dir, "8", `---
id: "8"
title: Other
status: todo
priority: low
type: bug
created_at: 2026-01-15T10:30:00Z
updated_at: 2026-01-15T10:30:00Z
area: cli
---

Other body.
`)

	idx := NewIndex(dir, st)
	cfg := &config.Config{TaskTypes: []string{"feature", "bug"}, RelationTypes: config.DefaultRelationTypes, ProjectFound: true}
	svc := task.NewService(st, st, st, idx, cfg.TaskTypes, cfg)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	return svc, st, dir
}

func fieldsOnDisk(t *testing.T, st *MarkdownStorage, id string) task.Fields {
	t.Helper()
	loaded, err := st.Load(id)
	if err != nil {
		t.Fatalf("Load(%s) error = %v", id, err)
	}
	return loaded.Fields
}

var wantSevenFields = task.Fields{
	"area":       "web",
	"complexity": "high",
	"count":      3,
	"flag":       true,
	"nested":     map[string]any{"x": 1},
	"notes":      "first\nsecond\n",
	"quoted":     "3",
}

// TestFieldsSurviveEveryMutatingFlow is the heart of the feature: a key the
// server knows nothing about must still be on disk after every flow that
// rewrites a record. The flows all round-trip a task freshly parsed from the
// file, so this holds without any per-flow code - which is exactly the
// property the test is here to keep true as flows are added.
func TestFieldsSurviveEveryMutatingFlow(t *testing.T) {
	flows := []struct {
		name string
		run  func(t *testing.T, svc *task.Service)
	}{
		{"update title", func(t *testing.T, svc *task.Service) {
			title := "Renamed"
			if _, err := svc.Update("7", &title, nil, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
		}},
		{"update status and priority", func(t *testing.T, svc *task.Service) {
			status := task.StatusInProgress
			prio := task.PriorityLow
			if _, err := svc.Update("7", nil, nil, &status, &prio, nil); err != nil {
				t.Fatal(err)
			}
		}},
		{"close with a resolution", func(t *testing.T, svc *task.Service) {
			if _, err := svc.Update("7", nil, nil, nil, nil, nil,
				task.WithResolution(task.ResolutionWontfix), task.WithResolutionNote("not now")); err != nil {
				t.Fatal(err)
			}
		}},
		{"verify", func(t *testing.T, svc *task.Service) {
			if _, err := svc.Update("7", nil, nil, nil, nil, nil, task.WithVerified(true)); err != nil {
				t.Fatal(err)
			}
		}},
		{"add a relation", func(t *testing.T, svc *task.Service) {
			if err := svc.AddRelation("7", "relates_to", "8"); err != nil {
				t.Fatal(err)
			}
		}},
		{"remove a relation", func(t *testing.T, svc *task.Service) {
			if err := svc.AddRelation("7", "blocked_by", "8"); err != nil {
				t.Fatal(err)
			}
			if err := svc.RemoveRelation("7", "blocked_by", "8"); err != nil {
				t.Fatal(err)
			}
		}},
		{"start and complete", func(t *testing.T, svc *task.Service) {
			if _, err := svc.StartTask("7"); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.CompleteTask("7"); err != nil {
				t.Fatal(err)
			}
		}},
		{"a cascade from deleting the other task", func(t *testing.T, svc *task.Service) {
			if err := svc.AddRelation("7", "blocked_by", "8"); err != nil {
				t.Fatal(err)
			}
			// Deleting 8 rewrites 7's frontmatter to drop the relation.
			if err := svc.Delete("8", false); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, flow := range flows {
		t.Run(flow.name, func(t *testing.T) {
			svc, st, _ := newFieldsBacklog(t)
			flow.run(t, svc)
			if got := fieldsOnDisk(t, st, "7"); !reflect.DeepEqual(got, wantSevenFields) {
				t.Errorf("fields after %s = %#v,\nwant %#v", flow.name, got, wantSevenFields)
			}
		})
	}
}

// TestFieldsSurviveArchiving covers the one flow that moves a record instead
// of rewriting it.
func TestFieldsSurviveArchiving(t *testing.T) {
	svc, st, dir := newFieldsBacklog(t)

	if _, err := svc.StartTask("7"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteTask("7"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchiveTask("7"); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "archive", "7", "7.md")); err != nil {
		t.Fatalf("archived record missing: %v", err)
	}

	archived, err := st.LoadArchived("7")
	if err != nil {
		t.Fatalf("LoadArchived() error = %v", err)
	}
	if !reflect.DeepEqual(archived.Fields, wantSevenFields) {
		t.Errorf("archived fields = %#v, want %#v", archived.Fields, wantSevenFields)
	}
}

// TestIndexCarriesFields pins the projection: the board, list_tasks,
// get_next_task and the CLI table all read index entries, so a field has to
// survive the entry round trip - and must not be aliased across it.
func TestIndexCarriesFields(t *testing.T) {
	_, st, dir := newFieldsBacklog(t)

	idx := NewIndex(dir, st)
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	var seven *task.Task
	for _, entry := range idx.All() {
		if entry.ID == "7" {
			seven = entry
		}
	}
	if seven == nil {
		t.Fatal("task 7 missing from the index")
	}
	if !reflect.DeepEqual(seven.Fields, wantSevenFields) {
		t.Errorf("index fields = %#v, want %#v", seven.Fields, wantSevenFields)
	}

	// Editing what the index handed out must not reach the entry behind it.
	seven.Fields["area"] = "tampered"
	for _, entry := range idx.All() {
		if entry.ID == "7" && entry.Fields["area"] != "web" {
			t.Errorf("the index aliased its entry's fields: area = %v", entry.Fields["area"])
		}
	}
}

// TestCreateWithFields covers the creation path, which is the one flow that
// builds a record from nothing rather than round-tripping one.
func TestCreateWithFields(t *testing.T) {
	svc, st, _ := newFieldsBacklog(t)

	created, err := svc.Create("With fields", "", task.PriorityHigh, "feature", "", "new",
		task.WithCreateFields(map[string]any{"complexity": "high", "count": 3, "flag": true}))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	want := task.Fields{"complexity": "high", "count": 3, "flag": true}
	if !reflect.DeepEqual(created.Fields, want) {
		t.Errorf("created fields = %#v, want %#v", created.Fields, want)
	}
	if got := fieldsOnDisk(t, st, "new"); !reflect.DeepEqual(got, want) {
		t.Errorf("fields on disk = %#v, want %#v", got, want)
	}

	// A reserved key fails the creation outright - no half-made task.
	if _, err := svc.Create("Bad", "", task.PriorityLow, "bug", "", "bad",
		task.WithCreateFields(map[string]any{"status": "done"})); err == nil {
		t.Error("Create() with a reserved field key = nil error, want one")
	}
	if _, err := st.Load("bad"); err == nil {
		t.Error("a refused Create still wrote a record")
	}
}

func TestUpdateFieldsMergeAndRemove(t *testing.T) {
	svc, st, _ := newFieldsBacklog(t)

	// A merge: complexity changes, the untouched keys stay.
	if _, err := svc.Update("7", nil, nil, nil, nil, nil,
		task.WithFields(map[string]any{"complexity": "low", "owner": "dev"})); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	got := fieldsOnDisk(t, st, "7")
	if got["complexity"] != "low" || got["owner"] != "dev" || got["area"] != "web" {
		t.Errorf("fields after a merge = %#v", got)
	}
	if len(got) != len(wantSevenFields)+1 {
		t.Errorf("fields after a merge = %#v, want one added key", got)
	}

	// Null and the empty string remove.
	if _, err := svc.Update("7", nil, nil, nil, nil, nil,
		task.WithFields(map[string]any{"owner": nil, "area": ""})); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	got = fieldsOnDisk(t, st, "7")
	if _, ok := got["owner"]; ok {
		t.Error("a null value did not remove the field")
	}
	if _, ok := got["area"]; ok {
		t.Error("an empty string did not remove the field")
	}

	// A rejected key leaves the record exactly as it was.
	before := fieldsOnDisk(t, st, "7")
	if _, err := svc.Update("7", nil, nil, nil, nil, nil,
		task.WithFields(map[string]any{"complexity": "medium", "Title": "x"})); err == nil {
		t.Error("Update() with a malformed key = nil error, want one")
	}
	if after := fieldsOnDisk(t, st, "7"); !reflect.DeepEqual(after, before) {
		t.Errorf("a refused update changed the record: %#v -> %#v", before, after)
	}
}

func TestListFieldFilter(t *testing.T) {
	svc, _, _ := newFieldsBacklog(t)

	parent := "0"
	cases := []struct {
		name   string
		filter map[string]string
		want   []string
	}{
		{"no filter", nil, []string{"7", "8"}},
		{"one value", map[string]string{"area": "web"}, []string{"7"}},
		{"the other value", map[string]string{"area": "cli"}, []string{"8"}},
		{"a number matches its digits", map[string]string{"count": "3"}, []string{"7"}},
		{"a bool matches its word", map[string]string{"flag": "true"}, []string{"7"}},
		{"two pairs are an AND", map[string]string{"area": "web", "complexity": "high"}, []string{"7"}},
		{"a contradictory AND matches nothing", map[string]string{"area": "web", "complexity": "low"}, nil},
		{"an unknown field matches nothing", map[string]string{"nope": "x"}, nil},
		{"case matters", map[string]string{"area": "WEB"}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var opts []task.ListOption
			if tc.filter != nil {
				opts = append(opts, task.WithFieldFilter(tc.filter))
			}
			got := svc.List(nil, nil, nil, &parent, nil, opts...)
			var ids []string
			for _, item := range got {
				ids = append(ids, item.ID)
			}
			if !reflect.DeepEqual(ids, tc.want) {
				t.Errorf("List() = %v, want %v", ids, tc.want)
			}
		})
	}
}
