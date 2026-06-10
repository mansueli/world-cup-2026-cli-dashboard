package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type teamsResponse struct {
	Teams []team `json:"teams"`
}

type team struct {
	FifaCode string `json:"fifa_code"`
	Flag     string `json:"flag"`
	NameEN   string `json:"name_en"`
}

func main() {
	resp, err := http.Get("https://worldcup26.ir/get/teams")
	if err != nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	var payload teamsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		panic(err)
	}

	entries := map[string][14][25]string{}
	codes := make([]string, 0, len(payload.Teams))

	client := &http.Client{}
	for _, t := range payload.Teams {
		code := strings.ToUpper(strings.TrimSpace(t.FifaCode))
		if len(code) != 3 || strings.TrimSpace(t.Flag) == "" {
			continue
		}
		if _, exists := entries[code]; exists {
			continue
		}

		imgResp, err := client.Get(t.Flag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s (%s): %v\n", code, t.NameEN, err)
			continue
		}
		imgBytes, _ := io.ReadAll(imgResp.Body)
		_ = imgResp.Body.Close()
		if imgResp.StatusCode < 200 || imgResp.StatusCode >= 300 {
			fmt.Fprintf(os.Stderr, "skip %s (%s): status %d\n", code, t.NameEN, imgResp.StatusCode)
			continue
		}

		img, _, err := image.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s (%s): decode error %v\n", code, t.NameEN, err)
			continue
		}

		entries[code] = sampleToGrid(img, 25, 14)
		codes = append(codes, code)
	}

	sort.Strings(codes)

	outPath := filepath.Join("ui", "flags", "generated_2026_flags.go")

	var buf strings.Builder
	buf.WriteString("package flags\n\nfunc init() {\n")
	for _, code := range codes {
		grid := entries[code]
		fmt.Fprintf(&buf, "\tcountryFlags[%q] = [14][25]string{\n", code)
		for y := 0; y < 14; y++ {
			buf.WriteString("\t\t{")
			for x := 0; x < 25; x++ {
				if x > 0 {
					buf.WriteString(", ")
				}
				fmt.Fprintf(&buf, "%q", grid[y][x])
			}
			buf.WriteString("},\n")
		}
		buf.WriteString("\t}\n")
	}
	buf.WriteString("}\n")

	if err := os.WriteFile(outPath, []byte(buf.String()), 0600); err != nil { //nolint:gosec
		panic(err)
	}
	fmt.Printf("generated %d flag entries in %s\n", len(codes), outPath)
}

func sampleToGrid(img image.Image, w, h int) [14][25]string {
	b := img.Bounds()
	var grid [14][25]string
	for y := 0; y < h; y++ {
		sy := b.Min.Y + int(float64(y)*float64(b.Dy())/float64(h))
		if sy >= b.Max.Y {
			sy = b.Max.Y - 1
		}
		for x := 0; x < w; x++ {
			sx := b.Min.X + int(float64(x)*float64(b.Dx())/float64(w))
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			r, g, b8, _ := img.At(sx, sy).RGBA()
			grid[y][x] = fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b8>>8)) //nolint:gosec // values are 0-255 after >>8
		}
	}
	return grid
}
