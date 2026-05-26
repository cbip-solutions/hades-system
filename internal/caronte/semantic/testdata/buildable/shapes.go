// SPDX-License-Identifier: MIT
package buildable

type Shape interface {
	Area() float64
}

type Circle struct{ R float64 }

func (c Circle) Area() float64 { return 3.14159 * c.R * c.R }

type Square struct{ Side float64 }

func (s *Square) Area() float64 { return s.Side * s.Side }

func TotalArea(shapes []Shape) float64 {
	helper()
	var sum float64
	for _, sh := range shapes {
		sum += sh.Area()
	}
	return sum
}

func helper() {}
