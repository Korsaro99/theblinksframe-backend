package catalog

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// MenuCount is one entry in a menu list (brand, style or price band).
type MenuCount struct {
	Key, Label string
	Count      int
}

// MenuSection is the counts for Men or Women: total plus breakdowns.
type MenuSection struct {
	Total                 int
	Brands, Styles, Bands []MenuCount
}

// Menu is the full set of counts the storefront menus need.
type Menu struct {
	Men, Women       MenuSection
	AccessoriesTotal int
	Brands           []MenuCount // every brand with products on sale
}

// Styles and bands keep a fixed display order; brands are sorted by name.
var (
	menuStyleOrder = []string{"AVIATOR", "ROUND", "SQUARE", "CAT_EYE", "WRAP"}
	menuBandOrder  = []string{BandUnder150, BandFrom150To250, BandOver250}
	menuStyleLabel = map[string]string{
		"AVIATOR": "Aviator",
		"ROUND":   "Round",
		"SQUARE":  "Square",
		"CAT_EYE": "Cat-eye",
		"WRAP":    "Wrap",
	}
	menuBandLabel = map[string]string{
		BandUnder150:     "Under GH₵150",
		BandFrom150To250: "GH₵150–250",
		BandOver250:      "GH₵250+",
	}
)

type menuRow struct {
	Category     string
	Gender       string
	Style        string
	PricePesewas int64
	BrandSlug    string
	BrandName    string
}

// Menu counts the products on sale.
func (s *Store) Menu(ctx context.Context) (Menu, error) {
	var rows []menuRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT p.category, p.gender, p.style, p.price_pesewas, b.slug AS brand_slug, b.name AS brand_name
		FROM products p JOIN brands b ON b.id = p.brand_id
		WHERE p.status = ? AND p.is_visible
		ORDER BY lower(b.name)`, StatusPublished).Scan(&rows).Error
	if err != nil {
		return Menu{}, fmt.Errorf("menu: %w", err)
	}

	out := Menu{
		Men:    emptySection(),
		Women:  emptySection(),
		Brands: []MenuCount{},
	}
	allBrands := map[string]*brandTally{}
	men := newSectionTally()
	women := newSectionTally()

	for _, r := range rows {
		incBrand(allBrands, r.BrandSlug, r.BrandName)

		if r.Category == CategoryAccessory {
			out.AccessoriesTotal++
		}
		if r.Gender == "MEN" || r.Gender == GenderUnisex {
			men.add(r)
		}
		if r.Gender == "WOMEN" || r.Gender == GenderUnisex {
			women.add(r)
		}
	}

	out.Men = men.toSection()
	out.Women = women.toSection()
	out.Brands = brandCounts(allBrands)
	return out, nil
}

func emptySection() MenuSection {
	return MenuSection{
		Brands: []MenuCount{},
		Styles: []MenuCount{},
		Bands:  []MenuCount{},
	}
}

type brandTally struct {
	name  string
	count int
}

type sectionTally struct {
	total  int
	brands map[string]*brandTally
	styles map[string]int
	bands  map[string]int
}

func newSectionTally() *sectionTally {
	return &sectionTally{
		brands: map[string]*brandTally{},
		styles: map[string]int{},
		bands:  map[string]int{},
	}
}

func (t *sectionTally) add(r menuRow) {
	t.total++
	incBrand(t.brands, r.BrandSlug, r.BrandName)
	if _, ok := menuStyleLabel[r.Style]; ok {
		t.styles[r.Style]++
	}
	band := PriceBand(r.PricePesewas)
	t.bands[band]++
}

func (t *sectionTally) toSection() MenuSection {
	return MenuSection{
		Total:  t.total,
		Brands: brandCounts(t.brands),
		Styles: orderedCounts(menuStyleOrder, menuStyleLabel, t.styles),
		Bands:  orderedCounts(menuBandOrder, menuBandLabel, t.bands),
	}
}

func incBrand(m map[string]*brandTally, slug, name string) {
	if b, ok := m[slug]; ok {
		b.count++
		return
	}
	m[slug] = &brandTally{name: name, count: 1}
}

func brandCounts(m map[string]*brandTally) []MenuCount {
	out := make([]MenuCount, 0, len(m))
	for slug, b := range m {
		if b.count == 0 {
			continue
		}
		out = append(out, MenuCount{Key: slug, Label: b.name, Count: b.count})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	return out
}

func orderedCounts(order []string, labels map[string]string, counts map[string]int) []MenuCount {
	out := make([]MenuCount, 0, len(order))
	for _, key := range order {
		n := counts[key]
		if n == 0 {
			continue
		}
		out = append(out, MenuCount{Key: key, Label: labels[key], Count: n})
	}
	return out
}
