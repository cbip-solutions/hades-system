// SPDX-License-Identifier: MIT
package tessera

type Adapter struct {
	projectID string
}

type Tile struct{}

func readTiles(projectID string) ([]Tile, error) { return nil, nil }

func (a *Adapter) ReadOwnTiles() ([]Tile, error) {
	return readTiles(a.projectID)
}
