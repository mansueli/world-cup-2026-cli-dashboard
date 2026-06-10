package flags

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mansueli/world-cup-2026-cli-dashboard/data"
)

func Render(countryCode string) string {
	countryFlag, ok := countryFlags[countryCode]
	if !ok {
		iso2 := data.TeamISO2ByCode[strings.ToUpper(strings.TrimSpace(countryCode))]
		if flag := emojiFlag(iso2); flag != "" {
			return flag
		}
		return ""
	}

	height := len(countryFlag)
	b := strings.Builder{}
	for y := 0; y < height; y += 2 {
		if y >= height-1 {
			// we expect a height of 14px, so it should never happen
			continue
		}

		for x := 0; x < 25; x++ {
			color1 := countryFlag[y][x]
			color2 := countryFlag[y+1][x]

			b.WriteString(lipgloss.
				NewStyle().
				SetString("▀").
				Foreground(lipgloss.Color(color1)).
				Background(lipgloss.Color(color2)).
				String(),
			)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func emojiFlag(iso2 string) string {
	iso2 = strings.ToUpper(strings.TrimSpace(iso2))
	if len(iso2) != 2 {
		return ""
	}

	b := []byte(iso2)
	if b[0] < 'A' || b[0] > 'Z' || b[1] < 'A' || b[1] > 'Z' {
		return ""
	}

	const regionalIndicatorA = 0x1F1E6
	r1 := rune(regionalIndicatorA + int(b[0]-'A'))
	r2 := rune(regionalIndicatorA + int(b[1]-'A'))
	return string([]rune{r1, r2})
}
