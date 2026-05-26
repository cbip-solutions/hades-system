// SPDX-License-Identifier: MIT
// Package embedded — manifest.go.
//
// Pure-data file: declares per-template Title + ShortDescription +
// RecommendedFor strings consumed by the Phase A wizard when rendering
// the "What kind of project?" question and by `zen new --list-templates`.
package embedded

type Metadata struct {
	Name             string
	Title            string
	ShortDescription string
	RecommendedFor   string
}

var EmbeddedTemplates = []Metadata{
	{
		Name:             "hermes-plugin-only",
		Title:            "Hermes plugin (skills + commands + hooks)",
		ShortDescription: "Pure Hermes plugin: skills/, commands/, hooks/, plugin.yaml. No daemon.",
		RecommendedFor:   "Operators extending Hermes without standing up a long-lived daemon.",
	},
	{
		Name:             "hermes-plugin+daemon",
		Title:            "Hermes plugin + companion daemon (Go)",
		ShortDescription: "Hermes plugin shape PLUS a Go daemon for stateful background work.",
		RecommendedFor:   "Multi-project orchestrators, persistent audit chains, long-lived MCP gateways.",
	},
	{
		Name:             "go-cli",
		Title:            "Go CLI (Cobra + Makefile)",
		ShortDescription: "Production-grade Go CLI with Cobra subcommands and Makefile targets.",
		RecommendedFor:   "DevOps tooling, internal CLIs, anything that benefits from `go install`.",
	},
	{
		Name:             "python-cli",
		Title:            "Python CLI (Click + pyproject.toml)",
		ShortDescription: "Click-based Python CLI with modern pyproject.toml layout.",
		RecommendedFor:   "Data tooling, scripting orchestrators, anything that benefits from `pipx install`.",
	},
	{
		Name:             "ts-saas",
		Title:            "TypeScript SaaS (Next.js + Hermes plugin)",
		ShortDescription: "Next.js application skeleton with a Hermes-plugin sidecar for agentic flows.",
		RecommendedFor:   "Web-facing products that want agentic features.",
	},
	{
		Name:             "ml-pipeline",
		Title:            "ML pipeline (uv + notebooks + Hermes plugin)",
		ShortDescription: "uv-managed Python project with notebooks/, datasets/, training/, plus Hermes plugin.",
		RecommendedFor:   "Research pipelines, training/eval workflows, anything where Hermes can mediate dataset moves.",
	},
}

func MetadataFor(name string) (Metadata, bool) {
	for _, m := range EmbeddedTemplates {
		if m.Name == name {
			return m, true
		}
	}
	return Metadata{}, false
}
