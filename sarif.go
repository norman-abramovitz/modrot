package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SARIF 2.1.0 output for GitHub code-scanning (issue #17, v1 file-level
// anchoring). Structs are a hand-rolled subset of the spec — only the
// fields code-scanning needs.

const sarifSchemaURI = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"

const (
	ruleArchived   = "archived-dependency"
	ruleDeprecated = "deprecated-dependency"
)

// SARIFInput groups the findings for one scanned go.mod file.
// GomodURI must be repo-relative with forward slashes.
type SARIFInput struct {
	GomodURI   string
	Results    []RepoStatus
	Deprecated []Module
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	ShortDescription     sarifText         `json:"shortDescription"`
	HelpURI              string            `json:"helpUri"`
	DefaultConfiguration sarifLevel        `json:"defaultConfiguration"`
	Properties           map[string]string `json:"properties"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifLevel struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level"`
	Message             sarifText         `json:"message"`
	Locations           []sarifLocation   `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

// sarifRules returns the two rule definitions, always both, in a fixed
// order (archived first) so output is deterministic.
func sarifRules() []sarifRule {
	const repoURL = "https://github.com/norman-abramovitz/modrot"
	return []sarifRule{
		{
			ID:                   ruleArchived,
			Name:                 "ArchivedDependency",
			ShortDescription:     sarifText{Text: "Dependency repository is archived on GitHub"},
			HelpURI:              repoURL + "#readme",
			DefaultConfiguration: sarifLevel{Level: "warning"},
			Properties:           map[string]string{"security-severity": "5.5"},
		},
		{
			ID:                   ruleDeprecated,
			Name:                 "DeprecatedDependency",
			ShortDescription:     sarifText{Text: "Dependency module is marked deprecated in its go.mod"},
			HelpURI:              repoURL + "#readme",
			DefaultConfiguration: sarifLevel{Level: "note"},
			Properties:           map[string]string{"security-severity": "3.0"},
		},
	}
}

// sarifGomodLocation returns a single path-only location for the go.mod file.
func sarifGomodLocation(uri string) []sarifLocation {
	return []sarifLocation{{
		PhysicalLocation: sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{URI: uri},
		},
	}}
}

// archivedMessage builds the human-readable message for an archived finding.
func archivedMessage(rs RepoStatus) string {
	var b strings.Builder
	b.WriteString(rs.Module.Path)
	if rs.Module.Version != "" {
		b.WriteString("@")
		b.WriteString(rs.Module.Version)
	}
	b.WriteString(" is archived on GitHub")
	var details []string
	if !rs.ArchivedAt.IsZero() {
		details = append(details, "archived "+rs.ArchivedAt.Format("2006-01-02"))
	}
	if !rs.PushedAt.IsZero() {
		details = append(details, "last pushed "+rs.PushedAt.Format("2006-01-02"))
	}
	if len(details) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(details, ", "))
		b.WriteString(")")
	}
	return b.String()
}

// deprecatedMessage builds the human-readable message for a deprecated finding.
func deprecatedMessage(m Module) string {
	var b strings.Builder
	b.WriteString(m.Path)
	if m.Version != "" {
		b.WriteString("@")
		b.WriteString(m.Version)
	}
	b.WriteString(" is deprecated")
	if m.Deprecated != "" {
		b.WriteString(": ")
		b.WriteString(m.Deprecated)
	}
	return b.String()
}

// buildSARIF assembles one SARIF run from per-go.mod inputs. Results is
// always non-nil so it serializes as [] rather than null.
func buildSARIF(inputs []SARIFInput) sarifLog {
	results := []sarifResult{}
	for _, in := range inputs {
		loc := sarifGomodLocation(in.GomodURI)
		for _, rs := range in.Results {
			if !rs.IsArchived {
				continue
			}
			results = append(results, sarifResult{
				RuleID:    ruleArchived,
				Level:     "warning",
				Message:   sarifText{Text: archivedMessage(rs)},
				Locations: loc,
				PartialFingerprints: map[string]string{
					"modrotFinding/v1": rs.Module.Path + ":archived",
				},
			})
		}
		for _, m := range in.Deprecated {
			results = append(results, sarifResult{
				RuleID:    ruleDeprecated,
				Level:     "note",
				Message:   sarifText{Text: deprecatedMessage(m)},
				Locations: loc,
				PartialFingerprints: map[string]string{
					"modrotFinding/v1": m.Path + ":deprecated",
				},
			})
		}
	}

	return sarifLog{
		Schema:  sarifSchemaURI,
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "modrot",
				Version:        version,
				InformationURI: "https://github.com/norman-abramovitz/modrot",
				Rules:          sarifRules(),
			}},
			Results: results,
		}},
	}
}

// PrintSARIF writes the SARIF log for the given inputs to stdout.
func PrintSARIF(inputs []SARIFInput) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(buildSARIF(inputs)); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error encoding SARIF: %v\n", err)
	}
}
