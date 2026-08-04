package parser

import (
	"fmt"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ledongthuc/pdf"
)

// extractPDFData extracts plain text from each page of a PDF using a
// pure-Go reader (CGO_ENABLED=0 friendly) and emits one "page" entity
// per page that carries content. The text itself is stored as a short
// excerpt on the entity so downstream tools can index it without
// re-parsing the binary format.
func extractPDFData(path string) ([]ir.Entity, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("unable to open pdf: %w", err)
	}
	defer f.Close()

	var entities []ir.Entity
	fonts := make(map[string]*pdf.Font)
	pages := r.NumPage()
	for i := 1; i <= pages; i++ {
		p := r.Page(i)
		// Cache fonts once per page to avoid re-parsing char maps.
		for _, name := range p.Fonts() {
			if _, ok := fonts[name]; !ok {
				font := p.Font(name)
				fonts[name] = &font
			}
		}

		text, err := p.GetPlainText(fonts)
		if err != nil {
			return nil, fmt.Errorf("pdf page %d extraction failed: %w", i, err)
		}
		if strings.TrimSpace(text) == "" {
			continue
		}

		meta := map[string]string{
			"page":       fmt.Sprintf("%d", i),
			"word_count": fmt.Sprintf("%d", len(strings.Fields(text))),
		}
		if excerpt := strings.Join(strings.Fields(text), " "); excerpt != "" {
			if len(excerpt) > 200 {
				excerpt = excerpt[:200]
			}
			meta["excerpt"] = excerpt
		}

		entities = append(entities, ir.Entity{
			ID:       fmt.Sprintf("%s#page:%d", path, i),
			Type:     "page",
			Name:     fmt.Sprintf("Page %d", i),
			Metadata: meta,
		})
	}

	return entities, nil
}
