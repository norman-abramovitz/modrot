package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildSARIF_ArchivedResult(t *testing.T) {
	inputs := []SARIFInput{{
		GomodURI: "go.mod",
		Results: []RepoStatus{
			{
				Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
				IsArchived: true,
				ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
				PushedAt:   time.Date(2021, 5, 5, 0, 0, 0, 0, time.UTC),
			},
			{
				Module:     Module{Path: "github.com/baz/qux", Version: "v2.0.0", Direct: false, Owner: "baz", Repo: "qux"},
				IsArchived: false,
				PushedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}}

	log := buildSARIF(inputs)

	if log.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", log.Version)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(log.Runs))
	}
	run := log.Runs[0]
	if run.Tool.Driver.Name != "modrot" {
		t.Errorf("driver name = %q, want modrot", run.Tool.Driver.Name)
	}
	if len(run.Tool.Driver.Rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(run.Tool.Driver.Rules))
	}
	if run.Tool.Driver.Rules[0].ID != "archived-dependency" || run.Tool.Driver.Rules[1].ID != "deprecated-dependency" {
		t.Errorf("rule ids = %q, %q", run.Tool.Driver.Rules[0].ID, run.Tool.Driver.Rules[1].ID)
	}
	if run.Tool.Driver.Rules[0].Properties["security-severity"] != "5.5" {
		t.Errorf("archived security-severity = %q, want 5.5", run.Tool.Driver.Rules[0].Properties["security-severity"])
	}

	if len(run.Results) != 1 {
		t.Fatalf("results = %d, want 1 (only the archived module)", len(run.Results))
	}
	r := run.Results[0]
	if r.RuleID != "archived-dependency" {
		t.Errorf("ruleId = %q", r.RuleID)
	}
	if r.Level != "warning" {
		t.Errorf("level = %q, want warning", r.Level)
	}
	if r.Message.Text != "github.com/foo/bar@v1.0.0 is archived on GitHub (archived 2024-07-22, last pushed 2021-05-05)" {
		t.Errorf("message = %q", r.Message.Text)
	}
	if len(r.Locations) != 1 || r.Locations[0].PhysicalLocation.ArtifactLocation.URI != "go.mod" {
		t.Errorf("locations = %+v, want one uri go.mod", r.Locations)
	}
	if r.PartialFingerprints["modrotFinding/v1"] != "github.com/foo/bar:archived" {
		t.Errorf("fingerprint = %q", r.PartialFingerprints["modrotFinding/v1"])
	}
}

func TestBuildSARIF_NoFindings(t *testing.T) {
	log := buildSARIF([]SARIFInput{{GomodURI: "go.mod"}})
	if log.Runs[0].Results == nil {
		t.Error("results must be non-nil (serializes as [] not null)")
	}
	if len(log.Runs[0].Results) != 0 {
		t.Errorf("results = %d, want 0", len(log.Runs[0].Results))
	}
}

func TestBuildSARIF_ArchivedNoDates(t *testing.T) {
	inputs := []SARIFInput{{
		GomodURI: "go.mod",
		Results: []RepoStatus{{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
			IsArchived: true,
		}},
	}}
	r := buildSARIF(inputs).Runs[0].Results[0]
	if r.Message.Text != "github.com/foo/bar@v1.0.0 is archived on GitHub" {
		t.Errorf("message = %q", r.Message.Text)
	}
}

func TestBuildSARIF_Deprecated(t *testing.T) {
	inputs := []SARIFInput{{
		GomodURI: "go.mod",
		Deprecated: []Module{
			{Path: "github.com/old/lib", Version: "v1.2.0", Deprecated: "use github.com/new/lib instead"},
		},
	}}

	run := buildSARIF(inputs).Runs[0]
	if len(run.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(run.Results))
	}
	r := run.Results[0]
	if r.RuleID != "deprecated-dependency" {
		t.Errorf("ruleId = %q", r.RuleID)
	}
	if r.Level != "note" {
		t.Errorf("level = %q, want note", r.Level)
	}
	want := "github.com/old/lib@v1.2.0 is deprecated: use github.com/new/lib instead"
	if r.Message.Text != want {
		t.Errorf("message = %q, want %q", r.Message.Text, want)
	}
	if r.Locations[0].PhysicalLocation.ArtifactLocation.URI != "go.mod" {
		t.Errorf("uri = %q", r.Locations[0].PhysicalLocation.ArtifactLocation.URI)
	}
	if r.PartialFingerprints["modrotFinding/v1"] != "github.com/old/lib:deprecated" {
		t.Errorf("fingerprint = %q", r.PartialFingerprints["modrotFinding/v1"])
	}
}

func TestPrintSARIF_ValidDocument(t *testing.T) {
	inputs := []SARIFInput{{
		GomodURI: "go.mod",
		Results: []RepoStatus{{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
			IsArchived: true,
		}},
	}}

	output := captureStdout(t, func() {
		PrintSARIF(inputs)
	})

	var doc map[string]any
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	if doc["version"] != "2.1.0" {
		t.Errorf("version = %v", doc["version"])
	}
	if doc["$schema"] != sarifSchemaURI {
		t.Errorf("$schema = %v", doc["$schema"])
	}
	runs := doc["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
}
