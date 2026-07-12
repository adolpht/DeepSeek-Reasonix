// figma.go — Figma REST API client used by figma_import. Only the Go standard
// library is used.
//
// The Figma file API returns a deeply nested document tree. We recursively walk
// it and project each node into a flat LayoutTree: type, name, bbox, fills,
// typography, and children. This is the same shape design_analyze produces, so
// downstream design_to_code works on either source uniformly.
//
// Auth: a personal access token, sent as "X-Figma-Token" header. It comes from
// the figma_token argument or the FIGMA_TOKEN environment variable.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	figmaAPIBase   = "https://api.figma.com/v1/files/"
	figmaTimeout   = 60 * time.Second
)

// FigmaNode is the subset of the Figma document node we project from. Only the
// fields used by ExtractLayout are decoded; the rest are ignored.
type FigmaNode struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Children []FigmaNode `json:"children,omitempty"`

	AbsoluteBoundingBox *figmaBox `json:"absoluteBoundingBox,omitempty"`
	Fills               []figmaPaint `json:"fills,omitempty"`
	Style               *figmaStyle  `json:"style,omitempty"`
	Characters          string       `json:"characters,omitempty"`
}

type figmaBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type figmaPaint struct {
	Type  string  `json:"type"`
	Color *figmaColor `json:"color,omitempty"`
}

type figmaColor struct {
	R float64 `json:"r"`
	G float64 `json:"g"`
	B float64 `json:"b"`
	A float64 `json:"a"`
}

type figmaStyle struct {
	FontFamily string  `json:"fontFamily,omitempty"`
	FontSize   float64 `json:"fontSize,omitempty"`
	FontWeight float64 `json:"fontWeight,omitempty"`
	LineHeightPercent float64 `json:"lineHeightPercent,omitempty"`
}

// FigmaFileResponse is the top-level /v1/files/<key> response.
type FigmaFileResponse struct {
	Name     string    `json:"name"`
	LastModified string `json:"lastModified"`
	Document FigmaNode `json:"document"`
}

// FetchFigmaFile fetches a Figma file by its key and returns the document root.
func FetchFigmaFile(fileKey, token string) (FigmaNode, error) {
	if token == "" {
		return FigmaNode{}, fmt.Errorf("figma token is required (set FIGMA_TOKEN env or pass figma_token)")
	}
	if fileKey == "" {
		return FigmaNode{}, fmt.Errorf("figma file key is empty")
	}

	url := figmaAPIBase + fileKey
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return FigmaNode{}, fmt.Errorf("build figma request: %w", err)
	}
	req.Header.Set("X-Figma-Token", token)

	client := &http.Client{Timeout: figmaTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return FigmaNode{}, fmt.Errorf("figma API call failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return FigmaNode{}, fmt.Errorf("read figma response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return FigmaNode{}, fmt.Errorf("figma API returned status %d: %s", resp.StatusCode, truncate(string(raw), 400))
	}

	var file FigmaFileResponse
	if err := json.Unmarshal(raw, &file); err != nil {
		return FigmaNode{}, fmt.Errorf("decode figma response: %w", err)
	}
	return file.Document, nil
}

// ExtractLayout recursively projects a Figma document node into a LayoutTree.
// Empty structural nodes (CANVAS / PAGE without meaningful content) are skipped
// in favor of their children, keeping the tree compact for code generation.
func ExtractLayout(node FigmaNode) LayoutTree {
	tree := LayoutTree{
		Type: node.Type,
		Name: node.Name,
	}
	if node.AbsoluteBoundingBox != nil {
		tree.BBox = &LayoutBox{
			X: node.AbsoluteBoundingBox.X,
			Y: node.AbsoluteBoundingBox.Y,
			Width: node.AbsoluteBoundingBox.Width,
			Height: node.AbsoluteBoundingBox.Height,
		}
	}
	for _, fill := range node.Fills {
		if fill.Type == "SOLID" && fill.Color != nil {
			tree.Colors = append(tree.Colors, rgbaToHex(*fill.Color))
		}
	}
	if node.Style != nil {
		tree.Font = &LayoutFont{
			Family: node.Style.FontFamily,
			Size: node.Style.FontSize,
			Weight: figmaWeightName(node.Style.FontWeight),
		}
	}
	if node.Characters != "" {
		tree.Text = node.Characters
	}
	for _, child := range node.Children {
		// Skip intermediate canvas/page layers — project their children directly.
		if child.Type == "CANVAS" || child.Type == "PAGE" {
			for _, grand := range child.Children {
				tree.Children = append(tree.Children, ExtractLayout(grand))
			}
			continue
		}
		tree.Children = append(tree.Children, ExtractLayout(child))
	}
	return tree
}

// parseFigmaFileKey accepts any Figma URL shape and returns the file key:
//   - https://www.figma.com/file/<key>/Title
//   - https://www.figma.com/design/<key>/Title
//   - https://www.figma.com/proto/<key>/Title
//   - bare <key>
func parseFigmaFileKey(figmaURL string) (string, error) {
	s := strings.TrimSpace(figmaURL)
	if s == "" {
		return "", fmt.Errorf("figma_url is empty")
	}
	// Bare key: no slash, no protocol.
	if !strings.Contains(s, "/") {
		return s, nil
	}
	for _, seg := range []string{"/file/", "/design/", "/proto/"} {
		if idx := strings.Index(s, seg); idx >= 0 {
			rest := s[idx+len(seg):]
			if slash := strings.Index(rest, "/"); slash >= 0 {
				return rest[:slash], nil
			}
			return rest, nil
		}
	}
	return "", fmt.Errorf("could not extract file key from figma URL: %s", figmaURL)
}

// rgbaToHex converts a Figma RGBA color (0..1 floats) to #rrggbb. Alpha is
// dropped because fill hex is used for design-token display only.
func rgbaToHex(c figmaColor) string {
	r := clamp8(c.R * 255)
	g := clamp8(c.G * 255)
	b := clamp8(c.B * 255)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}

// figmaWeightName maps a numeric font weight to the conventional CSS name used
// in design tokens (400 → normal, 700 → bold, etc).
func figmaWeightName(w float64) string {
	switch {
	case w == 0:
		return ""
	case w < 300:
		return "lighter"
	case w < 400:
		return "light"
	case w < 500:
		return "normal"
	case w < 600:
		return "medium"
	case w < 700:
		return "semibold"
	case w < 800:
		return "bold"
	case w < 900:
		return "bolder"
	default:
		return "black"
	}
}

// resolveFigmaToken returns the token argument if non-empty, else FIGMA_TOKEN.
func resolveFigmaToken(arg string) string {
	if strings.TrimSpace(arg) != "" {
		return arg
	}
	return os.Getenv("FIGMA_TOKEN")
}
