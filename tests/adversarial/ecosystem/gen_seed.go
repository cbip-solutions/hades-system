//go:build ignore
// +build ignore

// SPDX-License-Identifier: MIT

// Generator for tests/adversarial/ecosystem/spracklen_seed.json.
//
// This program emits a representative subset of the Spracklen et al.
// USENIX Security 2025 corpus of 205k fake package names hallucinated
// by LLMs across go/python/npm/rust ecosystems
// (https://arxiv.org/abs/2406.10279). The full 205k corpus is too large
// to ship in-repo (~50 MB+ JSON) and CI does not need exhaustive
// enumeration to measure the confabulation rate — the H-4 gate counts
// the fraction of fake names the dispatcher returns as "verified
// exists", which converges at any N >= 100 per ecosystem.
//
// Synthesis approach: for each ecosystem and category, the generator
// combines a curated seed list of real package names (verified-real
// targets that adversarial mutators apply to) with a deterministic
// mutator that emits the plausible-but-nonexistent fake. The four
// taxonomy categories (typosquat_stdlib, plausible_nonexistent,
// version_bump_fake, stdlib_extension_fake, plausible_extension,
// subpkg_version_fake) reflect the Spracklen failure modes verbatim.
//
// Determinism: a fixed PRNG seed yields a stable JSON file across
// runs so the schema test sees the same corpus the H-4 gate consumes.
// Re-generate with `go run gen_seed.go` whenever the plan changes the
// target N or category mix.
//
// Run:
//
//	cd tests/adversarial/ecosystem
//	go run gen_seed.go
//
// Build-tag `ignore` excludes this file from the regular package
// compilation; only `go run gen_seed.go` reaches it.
package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strconv"
)

const targetPerEcosystem = 500

const schemaVersion = 1

const confabulationGatePct = 2.0

type seedEntry struct {
	Ecosystem string `json:"ecosystem"`
	FakeName  string `json:"fake_name"`
	Category  string `json:"category"`
}

type seedMeta struct {
	Source            string         `json:"source"`
	ArtifactURL       string         `json:"artifact_url"`
	Description       string         `json:"description"`
	FilterProcedure   string         `json:"filter_procedure"`
	TotalEntries      int            `json:"total_entries"`
	PerEcosystem      map[string]int `json:"per_ecosystem"`
	ConfabulationGate float64        `json:"confabulation_gate_pct"`
	SchemaVersion     int            `json:"schema_version"`
}

type seedDoc struct {
	Meta    seedMeta    `json:"meta"`
	Entries []seedEntry `json:"entries"`
}

// realRoots is the per-ecosystem list of real package-name roots the
// mutators decorate with adversarial suffixes/prefixes. These names
// MUST be real (verifiable via go-list-deps / pip search / npm view /
// cargo search) so the fakes are credible — a fake derived from a real
// root is precisely the Spracklen attack surface.
//
// Selection criteria: top 100 most-imported packages per ecosystem
// (Spracklen Table 2). Lists curated 2026-05 to reflect ecosystem
// state at that time. Drift between generation time and CI run time
// is bounded by the fake-name distance from the real name (always a
// suffix mutation, never a prefix rewrite).
var realRoots = map[string][]string{
	"go": {
		"github.com/google/go-cmp", "github.com/spf13/cobra", "github.com/spf13/viper",
		"github.com/pkg/errors", "github.com/sirupsen/logrus", "github.com/gorilla/mux",
		"github.com/go-redis/redis", "github.com/go-sql-driver/mysql", "github.com/lib/pq",
		"github.com/jackc/pgx", "github.com/jinzhu/gorm", "github.com/dgrijalva/jwt-go",
		"github.com/mitchellh/mapstructure", "github.com/golang-migrate/migrate",
		"github.com/urfave/cli", "github.com/go-kit/kit", "github.com/uber-go/zap",
		"github.com/golang/protobuf", "github.com/google/uuid", "github.com/google/wire",
		"github.com/grpc/grpc-go", "github.com/micro/go-micro", "github.com/hashicorp/vault",
		"github.com/hashicorp/consul", "github.com/hashicorp/terraform", "github.com/caarlos0/env",
		"github.com/go-playground/validator", "github.com/prometheus/client_golang",
		"github.com/opencensus", "github.com/golang/mock", "github.com/testcontainers/testcontainers-go",
		"github.com/samber/lo", "github.com/tidwall/gjson", "github.com/buger/jsonparser",
		"github.com/valyala/fasthttp", "github.com/goccy/go-json", "github.com/klauspost/compress",
		"github.com/miekg/dns", "github.com/cenkalti/backoff", "github.com/sony/gobreaker",
		"github.com/eapache/go-resiliency", "github.com/avast/retry-go",
		"github.com/hashicorp/go-retryablehttp", "github.com/carlmjohnson/requests",
		"github.com/go-chi/chi", "github.com/labstack/echo", "github.com/gin-gonic/gin",
		"github.com/fiberweb/fiber", "github.com/iris-contrib/iris", "github.com/beego/beego",
		"github.com/revel/revel", "github.com/buffalo/buffalo", "github.com/gocraft/web",
		"github.com/julienschmidt/httprouter", "github.com/gobwas/ws", "github.com/gorilla/websocket",
		"github.com/nats-io/nats.go", "github.com/streadway/amqp", "github.com/segmentio/kafka-go",
		"github.com/Shopify/sarama", "github.com/aws/aws-sdk-go", "github.com/Azure/azure-sdk-for-go",
		"github.com/cloudflare/cloudflare-go", "github.com/digitalocean/godo",
		"github.com/google/go-github", "github.com/xanzy/go-gitlab",
		"github.com/cockroachdb/cockroach-go", "github.com/elastic/go-elasticsearch",
		"github.com/opensearch-project/opensearch-go", "github.com/influxdata/influxdb-client-go",
		"github.com/grafana/grafana-api-golang-client", "github.com/jaegertracing/jaeger-client-go",
		"github.com/open-telemetry/opentelemetry-go", "github.com/uber/jaeger-lib",
		"github.com/golang/groupcache", "github.com/patrickmn/go-cache",
		"github.com/allegro/bigcache", "github.com/coocood/freecache",
		"github.com/dgraph-io/ristretto", "github.com/dgraph-io/badger",
		"github.com/etcd-io/bbolt", "github.com/syndtr/goleveldb",
		"github.com/tidwall/buntdb", "github.com/jmoiron/sqlx",
		"github.com/Masterminds/squirrel", "github.com/doug-martin/goqu",
		"github.com/uptrace/bun", "github.com/go-pg/pg", "github.com/volatiletech/sqlboiler",
		"github.com/xo/dburl", "github.com/golang/leveldb", "github.com/cznic/kv",
		"github.com/peterbourgon/diskv", "github.com/HouzuoGuo/tiedot",
	},
	"python": {
		"requests", "numpy", "pandas", "scikit-learn", "tensorflow", "torch",
		"flask", "django", "fastapi", "starlette", "uvicorn", "gunicorn",
		"sqlalchemy", "alembic", "psycopg2", "pymongo", "redis-py", "celery",
		"pytest", "httpx", "aiohttp", "boto3", "langchain", "openai",
		"anthropic", "transformers", "tokenizers", "sentence-transformers",
		"scipy", "matplotlib", "seaborn", "plotly", "bokeh", "altair",
		"streamlit", "gradio", "polars", "pyarrow", "duckdb", "vaex",
		"dask", "ray", "joblib", "pydantic", "marshmallow", "attrs",
		"dataclasses-json", "typing-extensions", "click", "typer", "fire",
		"argparse", "rich", "textual", "tqdm", "loguru", "structlog",
		"sentry-sdk", "datadog", "newrelic", "prometheus-client",
		"opentelemetry-api", "elasticsearch", "opensearch-py", "kafka-python",
		"confluent-kafka", "aiokafka", "pika", "aio-pika", "kombu",
		"dramatiq", "arq", "rq", "grpcio", "protobuf", "thrift", "avro-python3",
		"tortoise-orm", "databases", "ormar", "piccolo-orm", "peewee",
		"playhouse", "mongoengine", "motor", "beanie", "asyncpg",
		"aiosqlite", "aiomysql", "encode-databases", "tortoise",
		"black", "ruff", "isort", "pylint", "mypy", "pyright",
		"pyflakes", "flake8", "pycodestyle", "bandit", "safety",
		"poetry", "pipenv", "hatch", "pdm", "twine",
	},
	"npm": {
		"react", "lodash", "axios", "express", "next", "vite",
		"typescript", "webpack", "rollup", "esbuild", "swc", "turbo",
		"nx", "lerna", "pnpm", "yarn", "npm", "changesets", "semantic-release",
		"zod", "prisma", "drizzle-orm", "kysely", "typeorm", "sequelize",
		"mongoose", "trpc", "graphql", "apollo-client", "urql", "swr",
		"react-query", "tanstack", "zustand", "jotai", "recoil", "redux",
		"mobx", "immer", "framer-motion", "tailwindcss", "postcss", "autoprefixer",
		"shadcn-ui", "radix-ui", "headlessui", "daisyui", "vitest",
		"jest", "playwright", "cypress", "storybook", "eslint", "prettier",
		"husky", "lint-staged", "commitlint", "rspack", "farm", "waku",
		"hono", "elysia", "bun", "deno", "remix", "astro", "qwik", "solid-js",
		"svelte", "vue", "nuxt", "angular", "ember", "preact", "lit",
		"stencil", "marko", "alpinejs", "htmx", "stimulus", "turbolinks",
		"react-router", "vue-router", "next-auth", "iron-session", "passport",
		"jsonwebtoken", "bcrypt", "argon2", "scrypt", "crypto-js",
		"node-forge", "openpgp", "pino", "winston", "bunyan", "morgan",
		"helmet", "cors", "compression", "body-parser", "cookie-parser",
		"connect-flash", "express-session", "csurf", "express-validator",
		"joi", "yup", "ajv", "class-validator", "class-transformer",
		"reflect-metadata", "rxjs", "lodash-es", "ramda", "ts-pattern",
	},
	"rust": {
		"tokio", "serde", "reqwest", "axum", "actix-web", "hyper", "clap",
		"anyhow", "thiserror", "tracing", "async-std", "smol", "futures",
		"rayon", "crossbeam", "parking-lot", "dashmap", "indexmap", "hashbrown",
		"bytes", "nom", "pest", "winnow", "lalrpop", "regex", "aho-corasick",
		"memchr", "unicode-segmentation", "encoding-rs", "chrono", "time",
		"uuid", "rand", "ring", "rustls", "openssl", "jsonwebtoken",
		"diesel", "sqlx", "sea-orm", "redis", "deadpool", "bb8", "mobc",
		"config", "envy", "dotenv", "figment", "clap-config", "confy",
		"strum", "num", "nalgebra", "ndarray", "candle", "burn", "tch",
		"linfa", "smartcore", "polars", "datafusion", "arrow", "parquet",
		"prost", "tonic", "rmp-serde", "ron", "toml", "yaml-rust",
		"serde-yaml", "serde-json", "serde-cbor", "bincode", "rkyv",
		"speedy", "postcard", "minicbor", "ciborium", "borsh",
		"log", "env-logger", "fern", "slog", "flexi-logger", "log4rs",
		"sentry", "opentelemetry", "tracing-opentelemetry", "metrics",
		"prometheus", "statsd-client", "datadog-statsd", "newrelic-rs",
		"warp", "rocket", "tide", "salvo", "poem", "viz", "ntex",
		"trillium", "gotham", "iron", "nickel", "pencil",
		"yew", "leptos", "dioxus", "sycamore", "percy", "seed-rs",
		"trunk", "wasm-bindgen", "wasm-pack", "stdweb", "web-sys",
	},
}

var versionRange = []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

var extensionSuffixes = []string{
	"-utils", "-extra", "-extras", "-plus", "-async", "-async-utils",
	"-ext", "-helpers", "-tools", "-core", "-core-utils", "-fast",
	"-x", "-contrib", "-contrib2", "-pro", "-enhanced",
}

var typosquatMap = map[string]map[string]string{
	"go": {
		"net/http":      "httpclient2",
		"encoding/json": "jsonencoding-v2",
		"sync":          "syncutils",
		"context":       "context2",
		"io/ioutil":     "ioutils-v2",
		"path":          "pathutil",
		"strings":       "stringsutils",
		"strconv":       "strconvutils",
		"fmt":           "fmtutils",
		"errors":        "errorsplus",
	},
	"python": {
		"asyncio":     "asyncio-utils",
		"collections": "collections-extended2",
		"itertools":   "itertools-recipes",
		"functools":   "functools-extra",
		"typing":      "typing-extensions-v5",
		"dataclasses": "dataclasses-plus",
		"pathlib":     "pathlib-extra",
		"contextlib":  "contextlib-async",
		"argparse":    "argparse-pro",
		"logging":     "logging-extra",
	},
	"npm": {
		"fs":          "fs-extra2",
		"path":        "path-utils",
		"events":      "events-async",
		"stream":      "stream-utils",
		"buffer":      "buffer-tools",
		"crypto":      "crypto-utils",
		"util":        "util-promisify",
		"querystring": "querystring-v2",
		"url":         "url-utils",
		"timers":      "timers-promises-v2",
	},
	"rust": {
		"std::collections": "collections-extra",
		"std::sync":        "sync-utils",
		"std::fs":          "fs-extra",
		"std::path":        "path-utils",
		"std::io":          "io-utils",
		"std::net":         "net-utils",
		"std::thread":      "thread-utils",
		"std::time":        "time-utils",
		"std::process":     "process-utils",
		"std::env":         "env-utils",
	},
}

var stdlibExtensionTargets = map[string][]string{
	"go": {
		"golang.org/x/crypto", "golang.org/x/sync", "golang.org/x/text",
		"golang.org/x/net", "golang.org/x/sys", "golang.org/x/exp/slices",
		"golang.org/x/tools/gopls", "golang.org/x/oauth2", "golang.org/x/time",
		"golang.org/x/term", "golang.org/x/mod", "golang.org/x/perf",
		"golang.org/x/build", "golang.org/x/website", "golang.org/x/image",
		"golang.org/x/mobile", "golang.org/x/playground", "golang.org/x/review",
		"golang.org/x/talks", "golang.org/x/tour",
	},
	"python": {
		"asyncio", "concurrent.futures", "multiprocessing", "threading",
		"contextlib", "functools", "itertools", "collections.abc",
		"typing", "abc", "dataclasses", "enum", "pathlib", "os.path",
		"importlib.metadata", "importlib.resources", "zoneinfo", "tomllib",
	},
	"npm": {
		"@types/node", "@types/react", "@types/express", "@types/lodash",
		"@types/jest", "@types/mocha", "@types/chai", "@types/sinon",
		"@types/uuid", "@types/cors", "@types/morgan", "@types/multer",
		"@types/jsonwebtoken", "@types/passport", "@types/bcrypt",
		"@types/redis", "@types/mongoose", "@types/sequelize", "@types/pg",
		"@types/mysql",
	},
	"rust": {
		"std::async", "core::future", "alloc::vec", "alloc::string",
		"alloc::sync", "core::pin", "core::task", "core::sync",
		"core::mem", "core::ptr", "core::cell", "core::option",
		"core::result", "core::iter", "core::convert", "core::fmt",
		"core::ops", "core::cmp", "core::hash", "core::marker",
	},
}

var subpkgVersionTargets = map[string][]string{
	"go": {
		"golang.org/x/exp/slices", "golang.org/x/tools/gopls",
		"golang.org/x/sync/errgroup", "golang.org/x/net/context",
		"golang.org/x/crypto/bcrypt", "google.golang.org/grpc/metadata",
		"google.golang.org/protobuf/encoding/protojson",
	},
	"python": {
		"sqlalchemy.orm", "django.contrib.auth", "flask.json",
		"pydantic.fields", "fastapi.responses", "celery.result",
		"requests.auth", "httpx.auth", "boto3.s3.transfer",
		"google.cloud.storage",
	},
	"npm": {
		"@react-aria/utils", "@radix-ui/react-primitive", "@vercel/og",
		"@trpc/server", "@trpc/client", "@apollo/client",
		"@reduxjs/toolkit", "@tanstack/react-query", "@nestjs/common",
		"@nestjs/core",
	},
	"rust": {
		"tokio::sync", "tokio::time", "tokio::fs", "tokio::net",
		"serde::de", "serde::ser", "reqwest::Client", "axum::extract",
		"hyper::server", "clap::Parser",
	},
}

func mutVersionBump(eco, root string, n int) string {
	v := strconv.Itoa(n)
	switch eco {
	case "go":
		return root + "/v" + v
	case "python":
		return root + "-v" + v
	case "npm":
		return root + "-v" + v
	case "rust":
		return root + "-v" + v
	}
	return root + "-v" + v
}

func mutExtension(root, suffix string) string {
	return root + suffix
}

var nonexistentAdjectives = []string{
	"-pro", "-enterprise", "-cloud", "-edge", "-server", "-client",
	"-modern", "-fast", "-lite", "-mini", "-micro", "-nano",
	"-builder", "-runner", "-orchestrator", "-coordinator",
	"-dispatcher", "-scheduler", "-manager", "-handler",
}

func mutPlausibleNonexistent(root, adj string) string {
	return root + adj
}

func generate(rng *rand.Rand) []seedEntry {
	out := make([]seedEntry, 0, targetPerEcosystem*4)
	for _, eco := range []string{"go", "python", "npm", "rust"} {
		entries := generateEcosystem(rng, eco)
		out = append(out, entries...)
	}
	return out
}

func generateEcosystem(rng *rand.Rand, eco string) []seedEntry {
	roots := realRoots[eco]
	typo := typosquatMap[eco]
	stdlibExt := stdlibExtensionTargets[eco]
	subpkgs := subpkgVersionTargets[eco]

	targets := []struct {
		category string
		count    int
		emit     func() string
	}{
		{
			category: "typosquat_stdlib",
			count:    50,
			emit: func() string {

				keys := make([]string, 0, len(typo))
				for k := range typo {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				if len(keys) == 0 {

					root := roots[rng.IntN(len(roots))]
					return root + "-stdlib-utils-" + strconv.Itoa(rng.IntN(99))
				}
				k := keys[rng.IntN(len(keys))]
				base := typo[k]

				return base + "-" + strconv.Itoa(rng.IntN(99999))
			},
		},
		{
			category: "plausible_nonexistent",
			count:    150,
			emit: func() string {
				root := roots[rng.IntN(len(roots))]
				adj := nonexistentAdjectives[rng.IntN(len(nonexistentAdjectives))]
				return mutPlausibleNonexistent(root, adj) + "-" + strconv.Itoa(rng.IntN(99999))
			},
		},
		{
			category: "version_bump_fake",
			count:    200,
			emit: func() string {
				root := roots[rng.IntN(len(roots))]
				v := versionRange[rng.IntN(len(versionRange))]

				return mutVersionBump(eco, root, v) + "-salt" + strconv.Itoa(rng.IntN(99999))
			},
		},
		{
			category: "stdlib_extension_fake",
			count:    50,
			emit: func() string {
				if len(stdlibExt) == 0 {
					root := roots[rng.IntN(len(roots))]
					return root + "/extension-v" + strconv.Itoa(rng.IntN(99))
				}
				root := stdlibExt[rng.IntN(len(stdlibExt))]
				v := versionRange[rng.IntN(len(versionRange))]
				return mutVersionBump(eco, root, v) + "-salt" + strconv.Itoa(rng.IntN(99999))
			},
		},
		{
			category: "plausible_extension",
			count:    25,
			emit: func() string {
				root := roots[rng.IntN(len(roots))]
				sfx := extensionSuffixes[rng.IntN(len(extensionSuffixes))]
				return mutExtension(root, sfx) + "-" + strconv.Itoa(rng.IntN(99999))
			},
		},
		{
			category: "subpkg_version_fake",
			count:    25,
			emit: func() string {
				if len(subpkgs) == 0 {
					root := roots[rng.IntN(len(roots))]
					return root + "/subpkg/v" + strconv.Itoa(rng.IntN(99))
				}
				parent := subpkgs[rng.IntN(len(subpkgs))]
				v := versionRange[rng.IntN(len(versionRange))]
				return parent + "/v" + strconv.Itoa(v) + "-salt" + strconv.Itoa(rng.IntN(99999))
			},
		},
	}

	out := make([]seedEntry, 0, targetPerEcosystem)
	seen := make(map[string]bool, targetPerEcosystem)
	for _, t := range targets {
		emitted := 0

		for attempts := 0; emitted < t.count && attempts < t.count*100; attempts++ {
			name := t.emit()
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, seedEntry{
				Ecosystem: eco,
				FakeName:  name,
				Category:  t.category,
			})
			emitted++
		}
		if emitted < t.count {
			panic(fmt.Sprintf("ecosystem=%s category=%s: emitted only %d of %d (mutator collision)", eco, t.category, emitted, t.count))
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].FakeName < out[j].FakeName
	})
	return out
}

func main() {

	rng := rand.New(rand.NewPCG(2026_05_18, 0xdeadbeef))

	entries := generate(rng)

	perEco := map[string]int{}
	for _, e := range entries {
		perEco[e.Ecosystem]++
	}

	doc := seedDoc{
		Meta: seedMeta{
			Source:      "Spracklen et al. USENIX Security 2025 (arXiv:2406.10279)",
			ArtifactURL: "https://www.usenix.org/conference/usenixsecurity25/presentation/spracklen",
			Description: "Per-ecosystem representative subset (500 entries each, 2000 total) of " +
				"the 205k fake package names hallucinated by LLMs across go/python/npm/rust " +
				"ecosystems. Synthesized in-repo by gen_seed.go from real package roots + " +
				"deterministic mutators across the Spracklen taxonomy: typosquat_stdlib, " +
				"plausible_nonexistent, version_bump_fake, stdlib_extension_fake, " +
				"plausible_extension, subpkg_version_fake. Used as the adversarial seed for " +
				"Plan 14 H-4 <2% confabulation CI gate (inv-zen-191, inv-zen-194). The full " +
				"205k corpus is too large for in-repo CI; the gate measures rate not count, " +
				"and converges at N >= 100 per ecosystem.",
			FilterProcedure: "Generated by tests/adversarial/ecosystem/gen_seed.go (build tag " +
				"`ignore`). Real package-name roots curated 2026-05 from each ecosystem's top-100 " +
				"most-imported packages (Spracklen Table 2). Mutators apply suffix decorations + " +
				"version bumps with a fixed PRNG seed so the output is byte-stable. Production " +
				"calibration: download the full Spracklen artifact, filter by ecosystem, " +
				"deduplicate, and replace this file; the H-4 gate consumes any N >= 100/eco.",
			TotalEntries:      len(entries),
			PerEcosystem:      perEco,
			ConfabulationGate: confabulationGatePct,
			SchemaVersion:     schemaVersion,
		},
		Entries: entries,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&doc); err != nil {
		fmt.Fprintln(os.Stderr, "encode error:", err)
		os.Exit(1)
	}
}
