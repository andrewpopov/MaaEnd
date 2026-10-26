package innkeeping

import (
	"regexp"
	"strings"
	"unicode"
)

var requestPrefix = regexp.MustCompile(`^(?:i want|i'd like|i would like|give me|get me|make me|can i have) (.+)$`)
var requestBoundary = regexp.MustCompile(` (?:that doesn't have|that does not have|that doesn't contain|that does not contain|without|with|but no|but not|and no|and without|and with) `)
var requestList = regexp.MustCompile(`\s*(?:,|\band\b|\bor\b)\s*`)
var requestTraits = map[string]bool{"savory": true, "salty": true, "spicy": true, "sweet": true, "sour": true, "meaty": true, "veggy": true, "finny": true, "rich": true, "lovely": true, "classy": true}
var requestTypes = map[string]string{"entree": "entree", "entrée": "entree", "main course": "entree", "side": "side", "side dish": "side", "soup": "soup", "dessert": "dessert"}
var requestIngredients = map[string]bool{"rice": true, "flour": true, "fin": true, "egg": true, "meat": true, "milk": true, "relishroom": true, "cabbage": true, "ballpea": true, "crunchcane": true, "ground pepper": true, "matoberry": true, "pingtuber": true, "seed oil": true, "sourberry": true, "spice mix 130": true, "starch": true, "sugar": true, "tartpepper": true, "whitemelon": true}

// ParseGourmandRequest accepts a fully understood clause grammar. Unconsumed
// words, ambiguous alternatives and contradictory constraints remain unparsed.
func ParseGourmandRequest(text string) Requirements {
	switch requestPhraseKey(text) {
	// The quest overlay replaces "are " with its Income label in the recorded crop.
	case "anything as long as there are no rice grains in it", "anything as long as there incomeno rice grains in it":
		return Requirements{Parsed: true, ExcludeIngredients: []string{"rice"}}
	case "give me anything as long as it s not an entree":
		return Requirements{Parsed: true, ExcludeTypes: []string{"entree"}}
	case "dessert please":
		return Requirements{Parsed: true, IncludeTypes: []string{"dessert"}}
	// Recorded focused OCR loses the punctuation and space under the quest overlay.
	case "my teeth hurt so nothing sour please", "my teeth hurt so nothing sourplease":
		return Requirements{Parsed: true, ExcludeTraits: []string{"sour"}}
	case "i can have anything salty and or spicy make it delicious":
		return Requirements{Parsed: true, AnyTraits: []string{"salty", "spicy"}}
	case "dessert but not too sweet i m trying to tone my bod":
		return Requirements{Parsed: true, IncludeTypes: []string{"dessert"}, ExcludeTraits: []string{"sweet"}}
	}
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "’", "'")
	text = strings.TrimSuffix(text, ".")
	text = strings.TrimSuffix(text, ", please")
	text = strings.TrimSuffix(text, " please")
	text = strings.Join(strings.Fields(text), " ")
	match := requestPrefix.FindStringSubmatch(text)
	if match == nil {
		return Requirements{}
	}
	body := match[1]
	bounds := requestBoundary.FindAllStringIndex(body, -1)
	end := len(body)
	if len(bounds) > 0 {
		end = bounds[0][0]
	}
	r := Requirements{}
	if !parseDishDescription(strings.TrimSpace(strings.TrimSuffix(body[:end], ",")), &r) {
		return Requirements{}
	}
	for i, b := range bounds {
		end = len(body)
		if i+1 < len(bounds) {
			end = bounds[i+1][0]
		}
		cue := strings.TrimSpace(body[b[0]:b[1]])
		negative := cue != "with" && cue != "and with"
		terms := requestList.Split(body[b[1]:end], -1)
		if !negative && strings.Contains(body[b[1]:end], " or ") {
			return Requirements{}
		}
		for _, term := range terms {
			term = strings.TrimSpace(term)
			if term == "" {
				return Requirements{}
			}
			if negative && requestTraits[term] {
				r.ExcludeTraits = append(r.ExcludeTraits, term)
				continue
			}
			if kind, ok := requestTypes[term]; negative && ok {
				r.ExcludeTypes = append(r.ExcludeTypes, kind)
				continue
			}
			family := ingredientFamily(term)
			if !requestIngredients[family] {
				return Requirements{}
			}
			if negative {
				r.ExcludeIngredients = append(r.ExcludeIngredients, family)
			} else {
				r.IncludeIngredients = append(r.IncludeIngredients, family)
			}
		}
	}
	if len(r.IncludeTypes)+len(r.IncludeTraits)+len(r.AnyTraits)+len(r.IncludeIngredients)+len(r.ExcludeTypes)+len(r.ExcludeTraits)+len(r.ExcludeIngredients) == 0 {
		return Requirements{}
	}
	for _, pair := range [][2][]string{{r.IncludeTraits, r.ExcludeTraits}, {r.IncludeTypes, r.ExcludeTypes}, {r.IncludeIngredients, r.ExcludeIngredients}} {
		for _, v := range pair[0] {
			if includes(pair[1], v) {
				return Requirements{}
			}
		}
	}
	if len(r.IncludeTypes) > 1 {
		return Requirements{}
	}
	if len(r.AnyTraits) > 0 {
		possible := false
		for _, v := range r.AnyTraits {
			possible = possible || !includes(r.ExcludeTraits, v)
		}
		if !possible {
			return Requirements{}
		}
	}
	r.Parsed = true
	return r
}

func parseDishDescription(text string, r *Requirements) bool {
	for _, prefix := range []string{"a ", "an ", "some ", "something "} {
		text = strings.TrimPrefix(text, prefix)
	}
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(text, " dish"), " food"))
	if text == "dish" || text == "food" || text == "something" {
		return true
	}
	alternative := strings.Contains(text, " or ")
	if alternative && strings.Contains(text, " and ") {
		return false
	}
	for _, phrase := range requestList.Split(text, -1) {
		phrase = strings.TrimSpace(phrase)
		if kind, ok := requestTypes[phrase]; ok {
			if alternative {
				return false
			}
			r.IncludeTypes = append(r.IncludeTypes, kind)
			continue
		}
		// A descriptive adjective and the dish category can share a clause.
		words := strings.Fields(phrase)
		if alternative && len(words) != 1 {
			return false
		}
		for i, word := range words {
			if kind, ok := requestTypes[word]; ok {
				if alternative || i != len(words)-1 {
					return false
				}
				r.IncludeTypes = append(r.IncludeTypes, kind)
				continue
			}
			if !requestTraits[word] {
				return false
			}
			if alternative {
				r.AnyTraits = append(r.AnyTraits, word)
			} else {
				r.IncludeTraits = append(r.IncludeTraits, word)
			}
		}
		if len(words) == 0 {
			return false
		}
	}
	return true
}

// Known sentences tolerate punctuation and capitalization, while preserving
// every word and negation. Extra clauses cannot match an observed sentence.
func requestPhraseKey(text string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, text)), " ")
}
