// SPDX-License-Identifier: MIT
// Package good contains fixtures that MUST NOT trigger the analyzer.
package good

import "fmt"

type Service struct {
	name string
}

func (s *Service) DoWork() (string, error) {
	return s.name, nil
}

func (s *Service) Process(input string) error {
	if input == "" {
		return fmt.Errorf("empty input")
	}
	return nil
}

func (s *Service) NopLog() {}

func PanicWithRuntimeMessage(x int) {
	if x < 0 {
		panic(fmt.Sprintf("invalid x=%d", x))
	}
}
