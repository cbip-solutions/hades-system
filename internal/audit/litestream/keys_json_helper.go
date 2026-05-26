// SPDX-License-Identifier: MIT
package litestream

import (
	"encoding/json"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

func jsonMarshalString(s string) ([]byte, error) {
	return json.Marshal(s)
}

func parseKeychainPayload(raw []byte) (S3Credentials, error) {
	var p struct {
		AccessKeyID     string `json:"accessKeyId"`
		SecretAccessKey string `json:"secretAccessKey"`
		Region          string `json:"region"`
		Endpoint        string `json:"endpoint"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return S3Credentials{}, err
	}
	if p.Region == "" {
		p.Region = "us-east-1"
	}
	return S3Credentials{
		AccessKeyID:     redact.NewSecret(p.AccessKeyID),
		SecretAccessKey: redact.NewSecret(p.SecretAccessKey),
		Region:          p.Region,
		Endpoint:        p.Endpoint,
	}, nil
}
