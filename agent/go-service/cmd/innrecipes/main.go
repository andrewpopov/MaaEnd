// Command innrecipes matches a complete Gourmand request against audited recipes.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/innkeeping"
	"os"
	"strings"
)

func main() {
	path := flag.String("catalog", "install/data/Innkeeping/recipes.json", "Audited recipe catalog")
	text := flag.String("request", "", "Complete request text")
	menu := flag.String("menu", "", "Comma-separated independently verified available dish IDs")
	flag.Parse()
	catalog, e := innkeeping.LoadCatalog(*path)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	requirement := innkeeping.ParseGourmandRequest(*text)
	all := innkeeping.MatchGourmandRecipes(requirement, catalog)
	available := []innkeeping.GourmandCandidate{}
	for _, c := range all {
		for _, id := range strings.Split(*menu, ",") {
			if c.ID == strings.TrimSpace(id) {
				available = append(available, c)
				break
			}
		}
	}
	result := struct {
		Request             string                         `json:"request"`
		Requirements        innkeeping.Requirements        `json:"requirements"`
		CatalogMatches      []innkeeping.GourmandCandidate `json:"catalog_matches"`
		VerifiedMenuMatches []innkeeping.GourmandCandidate `json:"verified_menu_matches"`
		InputAuthorized     bool                           `json:"input_authorized"`
	}{Request: *text, Requirements: requirement, CatalogMatches: all, VerifiedMenuMatches: available}
	if e = json.NewEncoder(os.Stdout).Encode(result); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if !requirement.Parsed {
		os.Exit(2)
	}
}
