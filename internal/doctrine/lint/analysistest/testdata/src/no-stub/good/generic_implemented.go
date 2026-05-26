// SPDX-License-Identifier: MIT
package good

type GenericService[T any] struct {
	value T
}

func (s *GenericService[T]) Get() T {
	return s.value
}

func (s *GenericService[T]) Set(v T) {
	s.value = v
}
