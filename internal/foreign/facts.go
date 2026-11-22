package foreign

import "path"

// Facts is what the driver reads from one TypeScript or JavaScript file. Lines and columns start
// at 1.
type Facts struct {
	Imports    []Import    `json:"imports"`
	Directives []Directive `json:"directives"`
	Exports    []Export    `json:"exports"`
	Calls      []Call      `json:"calls"`
	Tags       []Tag       `json:"tags"`
}

// Tag is a <title> or <meta> written as JSX in a component: not inside an <svg>, whose <title> is
// the tooltip of a graphic, and not a <meta itemProp>, which is microdata. Name is "title" or "meta".
type Tag struct {
	Name   string `json:"name"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Import is an import or re-export of the module Spec.
type Import struct {
	Spec   string `json:"spec"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Directive is a directive such as "use client".
type Directive struct {
	Value  string `json:"value"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Export is a named export. Kind is "function", "object", "string", "number", "boolean" or
// "other", and Value is the text of a string, number or boolean literal.
type Export struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Call is a call of a function by its name. From is the module the name is imported from, empty
// when the file does not import it.
type Call struct {
	Name   string `json:"name"`
	From   string `json:"from"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// CheckFacts returns the idioms of other frameworks in the file, whose path is relative to the
// application root. Imports and directives are read in every file. Exports and calls only in
// page.tsx, layout.tsx and layout_.tsx: other modules may export any name.
func CheckFacts(file string, f *Facts) []Finding {
	var out []Finding
	for _, i := range f.Imports {
		if r, ok := matchImport(i.Spec); ok {
			out = append(out, finding(file, i.Line, i.Column, r))
		}
	}
	for _, d := range f.Directives {
		switch d.Value {
		case "use client":
			out = append(out, finding(file, d.Line, d.Column, useClient))
		case "use server":
			out = append(out, finding(file, d.Line, d.Column, useServer))
		}
	}
	for _, tag := range f.Tags {
		out = append(out, finding(file, tag.Line, tag.Column, headInComponent(tag.Name)))
	}
	if pageFile(path.Base(file)) {
		for _, e := range f.Exports {
			if r, ok := exportRules[e.Name]; ok && r.matches(e) {
				out = append(out, finding(file, e.Line, e.Column, r.Rule))
			}
		}
		for _, c := range f.Calls {
			r, ok := callRules[c.Name]
			if !ok || c.From == "" {
				continue
			}
			// The import of a framework module says it already.
			if _, imported := matchImport(c.From); imported {
				continue
			}
			out = append(out, finding(file, c.Line, c.Column, r))
		}
	}
	return sortFindings(out)
}
