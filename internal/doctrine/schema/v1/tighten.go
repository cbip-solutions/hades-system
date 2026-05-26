// SPDX-License-Identifier: MIT
package v1

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

type TightenDirection int

const (
	TightenDirSkip TightenDirection = iota

	TightenDirDecrease

	TightenDirIncrease

	TightenDirTruth

	TightenDirAddOnly

	TightenDirBidirectional

	TightenDirRank
)

func (d TightenDirection) String() string {
	switch d {
	case TightenDirSkip:
		return "skip"
	case TightenDirDecrease:
		return "decrease"
	case TightenDirIncrease:
		return "increase"
	case TightenDirTruth:
		return "truth"
	case TightenDirAddOnly:
		return "add-only"
	case TightenDirBidirectional:
		return "bidirectional"
	case TightenDirRank:
		return "rank"
	}
	return fmt.Sprintf("unknown(%d)", d)
}

type TightenRule struct {
	Direction        TightenDirection
	RankList         []string
	RequiresOperator bool
	FieldType        reflect.Kind
}

var (
	tightenRegistryOnce sync.Once
	tightenRegistry     map[string]TightenRule
)

func TightenRegistry() map[string]TightenRule {
	tightenRegistryOnce.Do(buildTightenRegistry)
	out := make(map[string]TightenRule, len(tightenRegistry))
	for k, v := range tightenRegistry {
		out[k] = v
	}
	return out
}

func buildTightenRegistry() {
	tightenRegistry = make(map[string]TightenRule)
	walkTightenFields(reflect.TypeOf(Schema{}), "", func(path, tag string, ft reflect.Type) {
		rule, err := parseTightenTag(tag, ft)
		if err != nil {
			panic(fmt.Sprintf("doctrine: malformed tighten tag at %s: %v", path, err))
		}
		if rule.Direction == TightenDirSkip {
			return
		}
		tightenRegistry[path] = rule
	})
}

// walkTightenFields recurses Schema's struct hierarchy. For each leaf field
// (non-struct, non-pointer-to-struct), invokes fn with (dotted path, raw
// tighten tag, leaf type). Section parents (declared `tighten:"-"`) recurse
// into children but do NOT invoke fn.
func walkTightenFields(t reflect.Type, path string, fn func(path, tag string, ft reflect.Type)) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		fpath := path + "." + f.Name
		if path == "" {
			fpath = f.Name
		}
		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {

			walkTightenFields(ft, fpath, fn)
			continue
		}
		tag := f.Tag.Get("tighten")
		if tag == "" {

			panic(fmt.Sprintf("doctrine: leaf field %s missing tighten tag", fpath))
		}
		fn(fpath, tag, ft)
	}
}

func parseTightenTag(tag string, ft reflect.Type) (TightenRule, error) {
	if tag == "-" {
		return TightenRule{Direction: TightenDirSkip, FieldType: ft.Kind()}, nil
	}
	switch {
	case tag == "decrease":
		return TightenRule{Direction: TightenDirDecrease, FieldType: ft.Kind()}, nil
	case tag == "increase":
		return TightenRule{Direction: TightenDirIncrease, FieldType: ft.Kind()}, nil
	case tag == "truth":
		return TightenRule{Direction: TightenDirTruth, FieldType: ft.Kind()}, nil
	case tag == "add-only":
		if ft.Kind() != reflect.Slice {
			return TightenRule{}, fmt.Errorf("add-only requires slice field; got %v", ft.Kind())
		}
		return TightenRule{Direction: TightenDirAddOnly, FieldType: ft.Kind()}, nil
	case tag == "bidirectional" || strings.HasPrefix(tag, "bidirectional,"):
		rule := TightenRule{Direction: TightenDirBidirectional, FieldType: ft.Kind()}
		if rest, ok := strings.CutPrefix(tag, "bidirectional,"); ok {
			if rest == "requires-operator" {
				rule.RequiresOperator = true
			} else {
				return TightenRule{}, fmt.Errorf("bidirectional suffix must be 'requires-operator'; got %q", rest)
			}
		}
		return rule, nil
	case strings.HasPrefix(tag, "rank:"):
		body := strings.TrimPrefix(tag, "rank:")
		if body == "" {
			return TightenRule{}, fmt.Errorf("rank tag missing values")
		}
		ranks := strings.Split(body, ",")
		for i, r := range ranks {
			if strings.TrimSpace(r) == "" {
				return TightenRule{}, fmt.Errorf("rank tag value %d is empty", i)
			}
		}
		return TightenRule{Direction: TightenDirRank, RankList: ranks, FieldType: ft.Kind()}, nil
	}
	return TightenRule{}, fmt.Errorf("unknown tighten direction %q", tag)
}
