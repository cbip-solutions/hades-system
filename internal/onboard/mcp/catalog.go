// SPDX-License-Identifier: MIT
package mcp

type Tier int

const (
	TierUnknown Tier = iota

	TierMandatory

	TierUniversal

	TierSmart

	TierCatalog
)

func (t Tier) String() string {
	switch t {
	case TierMandatory:
		return "mandatory"
	case TierUniversal:
		return "universal"
	case TierSmart:
		return "smart-default"
	case TierCatalog:
		return "catalog"
	default:
		return "unknown"
	}
}

type Entry struct {
	Name string

	Tier Tier

	Description string

	RiskTier string

	Package string
}

var catalog = []Entry{

	{
		Name:        "hades-ctld",
		Tier:        TierMandatory,
		Description: "HADES daemon gateway (hades-ctld) — design choice aggregator",
		RiskTier:    "low",
		Package:     "local",
	},

	{
		Name:        "playwright",
		Tier:        TierUniversal,
		Description: "Browser automation (canonical post-SOTA-5; replaces ambiguous 'Browser MCP')",
		RiskTier:    "medium",
		Package:     "@playwright/mcp",
	},
	{
		Name:        "filesystem",
		Tier:        TierUniversal,
		Description: "Filesystem read/write MCP",
		RiskTier:    "high",
		Package:     "@modelcontextprotocol/server-filesystem",
	},
	{
		Name:        "github",
		Tier:        TierUniversal,
		Description: "GitHub official MCP",
		RiskTier:    "high",
		Package:     "@modelcontextprotocol/server-github",
	},

	{
		Name:        "prisma-postgres",
		Tier:        TierSmart,
		Description: "Prisma Postgres MCP (SOTA-5 replacement for archived Postgres MCP)",
		RiskTier:    "high",
		Package:     "@prisma/mcp",
	},
	{
		Name:        "sentry",
		Tier:        TierSmart,
		Description: "Sentry error tracking MCP",
		RiskTier:    "medium",
		Package:     "@sentry/mcp",
	},
	{
		Name:        "linear",
		Tier:        TierSmart,
		Description: "Linear issue tracker MCP",
		RiskTier:    "medium",
		Package:     "@linear/mcp",
	},
	{
		Name:        "memory",
		Tier:        TierSmart,
		Description: "Knowledge memory MCP (default-off when hades-ctld covers)",
		RiskTier:    "low",
		Package:     "@modelcontextprotocol/server-memory",
	},
	{
		Name:        "sequential-thinking",
		Tier:        TierSmart,
		Description: "Sequential reasoning aid (max-scope doctrine default)",
		RiskTier:    "low",
		Package:     "@modelcontextprotocol/server-sequential-thinking",
	},

	{
		Name:        "sqlite",
		Tier:        TierCatalog,
		Description: "SQLite MCP (community post-archive)",
		RiskTier:    "high",
		Package:     "community-sqlite-mcp",
	},
	{
		Name:        "graphql",
		Tier:        TierCatalog,
		Description: "GraphQL MCP (community)",
		RiskTier:    "medium",
		Package:     "community-graphql-mcp",
	},
	{
		Name:        "mysql",
		Tier:        TierCatalog,
		Description: "MySQL MCP (benborla/mcp-server-mysql)",
		RiskTier:    "high",
		Package:     "benborla/mcp-server-mysql",
	},
	{
		Name:        "openapi",
		Tier:        TierCatalog,
		Description: "OpenAPI MCP (community)",
		RiskTier:    "medium",
		Package:     "community-openapi-mcp",
	},
}

func AllEntries() []Entry {
	cp := make([]Entry, len(catalog))
	copy(cp, catalog)
	return cp
}

func ByName(name string) (Entry, bool) {
	for _, e := range catalog {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

func ByTier(t Tier) []Entry {
	var out []Entry
	for _, e := range catalog {
		if e.Tier == t {
			out = append(out, e)
		}
	}
	return out
}

func AssertAllTiered() {
	assertAllTiered(catalog)
}

func assertAllTiered(entries []Entry) {
	for _, e := range entries {
		if e.Name == "" {
			panic("mcp.AssertAllTiered: catalog entry has empty Name; programmer error")
		}
		if e.Tier == TierUnknown {
			panic("mcp.AssertAllTiered: MCP " + e.Name + " has Tier=0 (TierUnknown); programmer error (invariant substrate)")
		}
		if e.Tier < TierMandatory || e.Tier > TierCatalog {
			panic("mcp.AssertAllTiered: MCP " + e.Name + " has out-of-range Tier; programmer error (invariant substrate)")
		}
		if e.RiskTier == "" {
			panic("mcp.AssertAllTiered: MCP " + e.Name + " has empty RiskTier; design choice doctrine eval requires populated")
		}
	}
}

func init() {
	AssertAllTiered()
}
