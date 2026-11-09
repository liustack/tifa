package store

import (
	"encoding/json/v2"
	"os"
	"sort"
	"strings"
	"testing"
)

// i18next picks the key with _one for a count of 1 in English, and shows the name of the key when
// it is missing. So every key of the English texts with a form for other counts (_other) must have
// one for a count of 1 (_one).
func TestEveryPluralOfEnglishHasAFormForOne(t *testing.T) {
	data, err := os.ReadFile("_locales/en-US.json")
	if err != nil {
		t.Fatal(err)
	}
	var texts map[string]any
	if err := json.Unmarshal(data, &texts); err != nil {
		t.Fatal(err)
	}
	var missing []string
	var walk func(path string, m map[string]any)
	walk = func(path string, m map[string]any) {
		for k, v := range m {
			if sub, ok := v.(map[string]any); ok {
				walk(path+k+".", sub)
				continue
			}
			if base, ok := strings.CutSuffix(k, "_other"); ok {
				if _, ok := m[base+"_one"]; !ok {
					missing = append(missing, path+base+"_one")
				}
			}
		}
	}
	walk("", texts)
	sort.Strings(missing)
	for _, k := range missing {
		t.Errorf("en-US.json has no %s", k)
	}
}
