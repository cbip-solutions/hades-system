// SPDX-License-Identifier: MIT
package components

type Sparkline struct {
	Values []float64
	Width  int
}

func (s Sparkline) Render() string {
	if len(s.Values) == 0 {
		return ""
	}
	w := s.Width
	if w == 0 {
		w = 10
	}

	values := s.Values
	if len(values) > w {

		step := len(values) / w
		sampled := make([]float64, 0, w)
		for i := 0; i < len(values); i += step {
			sampled = append(sampled, values[i])
		}
		values = sampled[:min(w, len(sampled))]
	}

	min0, max0 := values[0], values[0]
	for _, v := range values {
		if v < min0 {
			min0 = v
		}
		if v > max0 {
			max0 = v
		}
	}

	bars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	out := make([]rune, len(values))
	rng := max0 - min0
	for i, v := range values {
		var idx int
		if rng > 0 {
			idx = int((v - min0) / rng * float64(len(bars)-1))
		}
		out[i] = bars[idx]
	}
	return string(out)
}
