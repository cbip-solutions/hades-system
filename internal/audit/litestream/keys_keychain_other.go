// go:build !darwin

// SPDX-License-Identifier: MIT

package litestream

func loadKeychainImpl(projectID string) (S3Credentials, error) {
	return S3Credentials{}, ErrKeychainUnsupported
}

func saveKeychainImpl(projectID string, creds S3Credentials) error {
	return ErrKeychainUnsupported
}

func deleteKeychainImpl(projectID string) error {
	return ErrKeychainUnsupported
}
