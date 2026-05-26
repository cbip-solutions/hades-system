// SPDX-License-Identifier: MIT
package bad

type GenericService[T any] struct{}

func (s *GenericService[T]) ProcessGeneric(input T) {
}
