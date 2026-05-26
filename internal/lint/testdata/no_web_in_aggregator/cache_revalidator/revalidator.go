// SPDX-License-Identifier: MIT
package cache

import (
	"net/http"
)

func revalidateSource(url string) error {
	req, _ := http.NewRequest("HEAD", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
