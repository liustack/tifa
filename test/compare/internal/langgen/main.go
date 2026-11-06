//go:build langgen

// Command langgen prints the tables of the matcher for TestLanguageData. It builds only with the
// tag langgen and an overlay that adds export.go.txt to golang.org/x/text/language as
// export_gen.go, since what it prints is in the package.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/text/language"
)

func main() {
	var in struct{ Languages, Scripts, Regions []string }
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		panic(err)
	}
	fmt.Print(language.ExportStoreMatch(in.Languages, in.Scripts, in.Regions))
}
