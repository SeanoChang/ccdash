package render

import (
	"math"
	"strings"
)

var (
	partials = []rune(" ▏▎▍▌▋▊▉█")
	spark    = []rune("▁▂▃▄▅▆▇█")
	// Indexed as dx*4+dy for the U+2800 braille block.
	brailleDots = [8]rune{0x01, 0x02, 0x04, 0x40, 0x08, 0x10, 0x20, 0x80}
)

func clamp01(value float64) float64 {
	if math.IsNaN(value) || value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

// Bar renders exactly width cells with eighth-cell precision.
func Bar(fraction float64, width int) string {
	if width <= 0 {
		return ""
	}
	exact := clamp01(fraction) * float64(width)
	full := int(exact)
	remainder := exact - float64(full)
	var out strings.Builder
	for i := 0; i < full && i < width; i++ {
		out.WriteRune('█')
	}
	written := full
	if written < width {
		index := int(remainder * 8)
		if index < 0 {
			index = 0
		}
		if index > 8 {
			index = 8
		}
		out.WriteRune(partials[index])
		written++
	}
	for ; written < width; written++ {
		out.WriteByte(' ')
	}
	return out.String()
}

func Sparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}
	minimum, maximum := values[0], values[0]
	for _, value := range values[1:] {
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}
	span := maximum - minimum
	var out strings.Builder
	for _, value := range values {
		index := 0
		if span > 0 {
			index = int((value - minimum) / span * float64(len(spark)-1))
		}
		if index < 0 {
			index = 0
		}
		if index >= len(spark) {
			index = len(spark) - 1
		}
		out.WriteRune(spark[index])
	}
	return out.String()
}

// Braille plots a connected series into a w-by-h cell grid at 2x4 subpixel
// resolution.
func Braille(series []float64, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	pixelWidth, pixelHeight := width*2, height*4
	grid := make([][]bool, pixelHeight)
	for y := range grid {
		grid[y] = make([]bool, pixelWidth)
	}
	if len(series) > 0 {
		minimum, maximum := series[0], series[0]
		for _, value := range series[1:] {
			minimum = math.Min(minimum, value)
			maximum = math.Max(maximum, value)
		}
		span := maximum - minimum
		previousY := -1
		for x := 0; x < pixelWidth; x++ {
			position := float64(x) * float64(len(series)-1) / float64(pixelWidth-1)
			left := int(position)
			right := left + 1
			if right >= len(series) {
				right = left
			}
			fraction := position - float64(left)
			value := series[left]*(1-fraction) + series[right]*fraction
			normalized := 0.5
			if span > 0 {
				normalized = (value - minimum) / span
			}
			y := int(math.Round((1 - normalized) * float64(pixelHeight-1)))
			grid[y][x] = true
			if previousY >= 0 {
				low, high := previousY, y
				if low > high {
					low, high = high, low
				}
				for bridge := low; bridge <= high; bridge++ {
					grid[bridge][x] = true
				}
			}
			previousY = y
		}
	}
	var out strings.Builder
	for cellY := 0; cellY < height; cellY++ {
		for cellX := 0; cellX < width; cellX++ {
			var mask rune
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					if grid[cellY*4+dy][cellX*2+dx] {
						mask |= brailleDots[dx*4+dy]
					}
				}
			}
			if mask == 0 {
				out.WriteByte(' ')
			} else {
				out.WriteRune(0x2800 + mask)
			}
		}
		if cellY < height-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
