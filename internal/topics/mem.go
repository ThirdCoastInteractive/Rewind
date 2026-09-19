package topics

type memCat struct {
	title  map[string]string
	alias  map[string]string
	origin map[string]string
}

func newMem(seedSlug, seedTitle string) *memCat {
	m := &memCat{
		title:  map[string]string{},
		alias:  map[string]string{},
		origin: map[string]string{},
	}
	if seedSlug != "" {
		m.EnsureTopic(seedSlug, seedTitle, "wiki")
		for _, a := range aliasesFor(seedSlug, seedTitle) {
			m.EnsureAlias(a.Norm, seedSlug, a.Raw, a.Source)
		}
	}
	return m
}

func (m *memCat) Lookup(norm string) (string, string, bool) {
	slug, ok := m.alias[norm]
	if !ok {
		return "", "", false
	}
	return slug, m.title[slug], true
}

func (m *memCat) EnsureTopic(slug, title, origin string) (string, bool) {
	if t, ok := m.title[slug]; ok {
		return t, false
	}
	m.title[slug] = title
	m.origin[slug] = origin
	m.alias[slug] = slug
	return title, true
}

func (m *memCat) EnsureAlias(norm, slug, raw, source string) {
	if _, ok := m.alias[norm]; ok {
		return
	}
	m.alias[norm] = slug
}
