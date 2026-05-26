// SPDX-License-Identifier: MIT
package bad

type Service struct{}

func (s *Service) Configure(opt string) {
}

func (s *Service) Process(input string) error {
	return nil
}

func (s *Service) Notify(name, msg string) {
}

func (s *Service) NopLog() {}

func (s *Service) Pong() {}

type IFace interface {
	DoWork() (string, error)
	Process(input string) error
}
