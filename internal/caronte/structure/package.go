// SPDX-License-Identifier: MIT
package structure

import (
	"path"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func packageID(n store.Node) string {
	if strings.TrimSpace(n.PackageID) != "" {
		return n.PackageID
	}
	fp := filepathToSlash(n.FilePath)
	dir := path.Dir(fp)
	if dir == "" || dir == "/" {
		return "."
	}
	return dir
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}
