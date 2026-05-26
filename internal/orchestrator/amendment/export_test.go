package amendment

import (
	"text/template"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func SetDrafterTemplate(d *TemplateDrafter, t *template.Template) {
	d.tmpl = t
}

func ParseADRIDForTest(s string) (int, error) {
	return parseADRID(s)
}

func WalkForTest(err error, fn func(error)) {
	walk(err, fn)
}

func TightenViolationPayloadForTest(e eventlog.DoctrineTightenViolationRejected) map[string]any {
	return tightenViolationPayload(e)
}
