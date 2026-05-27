// SPDX-License-Identifier: MIT
package hadesday

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"time"
)

func Render(doc BriefDoc) string {
	if len(doc.Items) > MaxBriefItems {
		panic(fmt.Sprintf(
			"hadesday.Render: inv-hades-126 violation: len(items)=%d > MaxBriefItems=%d",
			len(doc.Items), MaxBriefItems))
	}
	if !IsSorted(doc.Items) {
		panic("hadesday.Render: inv-hades-127 violation: IsSorted(items) = false")
	}

	var out string
	switch doc.Type {
	case BriefTypeMorning:
		out = renderMorning(doc)
	case BriefTypeEOD:
		out = renderEOD(doc)
	case BriefTypeCheckPending:
		out = renderCheckPending(doc)
	default:
		panic(fmt.Sprintf("hadesday.Render: unknown BriefType %d", doc.Type))
	}

	if doc.Augmentation != nil {
		out += renderAugmentation(doc.Augmentation)
	}
	if doc.Knowledge != nil {
		out += renderKnowledge(doc.Knowledge)
	}
	if doc.Notifications != nil {
		out += renderNotifications(doc.Notifications)
	}

	return out
}

const morningTemplate = "# hades day — {{.Date.Format \"2006-01-02\"}} morning brief\n" +
	"{{$gates := .GroupGates}}" +
	"{{$costs := .GroupCosts}}" +
	"{{$activity := .GroupActivity}}" +
	"{{if $gates}}\n" +
	"## Pending operator action ({{len $gates}})\n" +
	"{{range $gates -}}\n" +
	"- **[{{.Project}}]** {{.Message}}\n" +
	"{{- if .Action}}\n" +
	"  → `{{.Action}}`\n" +
	"{{- end}}\n" +
	"{{end -}}" +
	"{{end}}" +
	"{{if $costs}}\n" +
	"## Cost watch ({{len $costs}})\n" +
	"{{range $costs -}}\n" +
	"- **[{{.Project}}]** {{.Message}}\n" +
	"{{end -}}" +
	"{{end}}" +
	"{{if $activity}}\n" +
	"## Activity ({{len $activity}})\n" +
	"{{range $activity -}}\n" +
	"- **[{{.Project}}]** {{.Message}}\n" +
	"{{end -}}" +
	"{{end}}" +
	"{{if gt .TruncatedCount 0}}\n" +
	"+ {{.TruncatedCount}} more in `hades inbox --since 24h`\n" +
	"{{end}}"

const eodTemplate = "# hades day — {{.Date.Format \"2006-01-02\"}} EOD digest\n" +
	"\n" +
	"## Per-project status ({{len .PerProjectStatus}} active)\n" +
	"{{range .PerProjectStatus}}\n" +
	"### {{.Alias}}{{if .AutonomousState}} (autonomous: {{.AutonomousState}}){{end}}\n" +
	"{{- if .HandoffSummary}}\n" +
	"- {{.HandoffSummary}}\n" +
	"{{- else}}\n" +
	"- No handoff posted today\n" +
	"{{- end}}\n" +
	"{{- range .Blockers}}\n" +
	"- Blocker: {{.}}\n" +
	"{{- end}}\n" +
	"{{- if .Tomorrow}}\n" +
	"- Tomorrow: {{.Tomorrow}}\n" +
	"{{- end}}\n" +
	"{{end}}\n" +
	"## Cross-project (1)\n" +
	"- Cost: ${{printf \"%.2f\" .CostWatchUSD}} total spend across projects today\n"

const checkPendingTemplate = "Next morning brief: {{.NextScheduledAt.Format \"2006-01-02 15:04:05\"}}\n" +
	"Pending items since last brief: {{.PendingActionNeeded}} action-needed, {{.PendingUrgent}} urgent\n"

type briefView struct {
	BriefDoc
}

func (v briefView) GroupGates() []BriefItem {
	return filterByRank(v.Items, RankOperatorGate, RankFailedScheduledJob, RankUrgentEvent)
}

func (v briefView) GroupCosts() []BriefItem {
	return filterByRank(v.Items, RankCostCapWarning)
}

func (v briefView) GroupActivity() []BriefItem {
	return filterByRank(v.Items, RankAutonomousMilestone, RankExternalActivity, RankInfoImmediate)
}

func filterByRank(items []BriefItem, ranks ...LeverageRank) []BriefItem {
	if len(items) == 0 {
		return nil
	}
	allowed := make(map[LeverageRank]struct{}, len(ranks))
	for _, r := range ranks {
		allowed[r] = struct{}{}
	}
	out := make([]BriefItem, 0, len(items))
	for _, it := range items {
		if _, ok := allowed[it.Rank]; ok {
			out = append(out, it)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var (
	morningTmpl      = template.Must(template.New("morning").Parse(morningTemplate))
	eodTmpl          = template.Must(template.New("eod").Parse(eodTemplate))
	checkPendingTmpl = template.Must(template.New("check-pending").Parse(checkPendingTemplate))
)

func mustExecute(tmpl *template.Template, data any) string {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("hadesday.Render: %s template exec: %v", tmpl.Name(), err))
	}
	return buf.String()
}

func renderMorning(doc BriefDoc) string {
	return mustExecute(morningTmpl, briefView{BriefDoc: doc})
}

func renderEOD(doc BriefDoc) string {
	return mustExecute(eodTmpl, doc)
}

func renderCheckPending(doc BriefDoc) string {
	return mustExecute(checkPendingTmpl, doc)
}

func renderAugmentation(s *AugmentationSection) string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Augmentation (Plan 11)\n\n")
	fmt.Fprintf(&b, "- total_cost: $%.2f USD\n", s.TotalCostUSD)
	fmt.Fprintf(&b, "- tokens_consumed: %s / %s",
		formatThousands(s.TokensConsumed), formatThousands(s.TokensCeiling))
	if s.TokensCeiling > 0 {
		pct := float64(s.TokensConsumed) / float64(s.TokensCeiling) * 100
		fmt.Fprintf(&b, " (%.0f%% of doctrine ceiling)", pct)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- kg_queries_fired: %d\n", s.KGQueriesFired)
	fmt.Fprintf(&b, "- cache_hit_rate: %.0f%%\n", s.CacheHitRate*100)
	if s.LastIndexedRFC3339 != "" {
		fmt.Fprintf(&b, "- last_indexed: %s", s.LastIndexedRFC3339)
		if t, err := time.Parse(time.RFC3339, s.LastIndexedRFC3339); err == nil {
			fmt.Fprintf(&b, " (%s ago)", humanizeDuration(time.Since(t)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderKnowledge(s *KnowledgeSection) string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Knowledge (Plan 11)\n\n")
	line := fmt.Sprintf("- fts5_docs: %s", formatThousands(s.FTS5Docs))
	if s.FTS5DocsDeltaSinceYesterday != 0 {
		line += fmt.Sprintf(" (+%d since yesterday)", s.FTS5DocsDeltaSinceYesterday)
	}
	b.WriteString(line + "\n")
	if s.AggregatorDBSizeMB > 0 {
		fmt.Fprintf(&b, "- aggregator_db_size_mb: %d\n", s.AggregatorDBSizeMB)
	}
	fmt.Fprintf(&b, "- promote_today: %d\n", s.PromoteToday)
	fmt.Fprintf(&b, "- cross_project_queries: %d", s.CrossProjectQueries)
	b.WriteString(" (max-scope+default visible; capa-firewall=0)\n")
	if s.LitestreamReplicaLagSec > 0 {
		lag := fmt.Sprintf("%ds", s.LitestreamReplicaLagSec)
		health := "healthy < 60s"
		if s.LitestreamReplicaLagSec >= 60 {
			health = "WARN >= 60s"
		}
		fmt.Fprintf(&b, "- litestream_replica_lag: %s (%s)\n", lag, health)
	}
	return b.String()
}

func renderNotifications(s *NotificationsSection) string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Notifications (Plan 11 — Hermes-routed)\n\n")
	if len(s.RoutesActive) > 0 {
		fmt.Fprintf(&b, "- routes_active: %s\n", strings.Join(s.RoutesActive, ", "))
	} else {
		b.WriteString("- routes_active: (none configured; see ~/.hermes/config.yaml)\n")
	}
	fmt.Fprintf(&b, "- pending_acks: %d", s.PendingAcks)
	if s.PendingAcks > 0 {
		b.WriteString(" (hades inbox --since 24h)")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- cost_cap_alerts: 50%%=%d, 80%%=%d, 100%%=%d\n",
		s.CostCap50Alerts, s.CostCap80Alerts, s.CostCap100Alerts)
	fmt.Fprintf(&b, "- caronte_health_digests: %d\n", s.CaronteHealthDigests)
	fmt.Fprintf(&b, "- hermes_dispatch_errors: %d", s.HermesDispatchErrors)
	if s.HermesDispatchErrors > 0 {
		b.WriteString(" (investigate: hades doctor hermes)")
	}
	b.WriteString("\n")
	return b.String()
}

func formatThousands(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
