// SPDX-License-Identifier: MIT

package cgosupplement

import (
	"encoding/json"
	"fmt"
	"os"
)

type Entry struct {
	Name          string
	Vendor        string
	License       string
	Version       string
	Type          string
	GoBinding     string
	VendorPath    string
	PlatformScope string
}

type Supplement struct {
	Path string

	Entries []Entry

	raw []byte
}

func Load(path string) (*Supplement, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read supplement %s: %w", path, err)
	}
	s := &Supplement{Path: path, raw: data}
	if err := s.parse(); err != nil {
		return nil, fmt.Errorf("parse supplement %s: %w", path, err)
	}
	return s, nil
}

func (s *Supplement) parse() error {
	var bom struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Components  []struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Version   string `json:"version"`
			Publisher string `json:"publisher"`
			Licenses  []struct {
				License struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"license"`
			} `json:"licenses"`
			Properties []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"properties"`
		} `json:"components"`
	}
	if err := json.Unmarshal(s.raw, &bom); err != nil {
		return err
	}
	if bom.BOMFormat != "CycloneDX" {
		return fmt.Errorf("bomFormat not CycloneDX: %q", bom.BOMFormat)
	}
	if bom.SpecVersion != "1.6" {
		return fmt.Errorf("specVersion not 1.6: %q", bom.SpecVersion)
	}
	for _, c := range bom.Components {
		e := Entry{
			Name:    c.Name,
			Vendor:  c.Publisher,
			Version: c.Version,
		}
		if len(c.Licenses) > 0 {
			if c.Licenses[0].License.ID != "" {
				e.License = c.Licenses[0].License.ID
			} else {
				e.License = c.Licenses[0].License.Name
			}
		}
		for _, p := range c.Properties {
			switch p.Name {
			case "hades-system:cgo-classification":
				e.Type = p.Value
			case "hades-system:go-binding":
				e.GoBinding = p.Value
			case "hades-system:vendor-path":
				e.VendorPath = p.Value
			case "hades-system:platform-scope":
				e.PlatformScope = p.Value
			}
		}
		s.Entries = append(s.Entries, e)
	}
	return nil
}
