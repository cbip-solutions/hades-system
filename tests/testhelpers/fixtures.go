// SPDX-License-Identifier: MIT
package testhelpers

import (
	"github.com/cbip-solutions/hades-system/internal/store"
)

func SampleEvent(typ, project string) store.EventRow {
	return store.EventRow{
		Type:        typ,
		Project:     project,
		PayloadJSON: `{"sample":true}`,
	}
}

func SampleProject(id, doctrine string) store.ProjectRow {
	return store.ProjectRow{
		ID:        id,
		Path:      "/path/to/projects/" + id,
		Execution: "mac",
		Doctrine:  doctrine,
	}
}
