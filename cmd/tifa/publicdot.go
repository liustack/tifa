package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/liustack/tifa/internal/bundle"
	"github.com/liustack/tifa/internal/codegen"
)

// hintListed is how many of the dot names left out of public/ the hint of tifa build lists.
const hintListed = 5

// dotNames says how many dot names there are and of what kind, such as "2 dot files", "1 dot
// folder" or "3 dot files or folders". A name that ends in / is a folder.
func dotNames(left []string) string {
	folders := 0
	for _, name := range left {
		if strings.HasSuffix(name, "/") {
			folders++
		}
	}
	s := ""
	if len(left) != 1 {
		s = "s"
	}
	switch folders {
	case 0:
		return fmt.Sprintf("%d dot file%s", len(left), s)
	case len(left):
		return fmt.Sprintf("%d dot folder%s", len(left), s)
	}
	return fmt.Sprintf("%d dot files or folders", len(left))
}

// publicHint is the line that tells which dot names of public/ are left out of the build and how to
// get one copied, empty when none is left out.
func publicHint(public string, left []string) string {
	if len(left) == 0 {
		return ""
	}
	shown := left[:min(len(left), hintListed)]
	list := strings.Join(shown, ", ")
	if more := len(left) - len(shown); more > 0 {
		list += fmt.Sprintf(" and %d more", more)
	}
	verb := "are"
	if len(left) == 1 {
		verb = "is"
	}
	return fmt.Sprintf("tifa: public: %s in %s/ %s not copied: %s. To copy one, list it in build.public_dotfiles in tifa.yaml.", dotNames(left), public, verb, list)
}

// publicLeftOut scans the public folder of the app at root as tifa build copies it. It fails when
// build.public_dotfiles names something that is not there, and returns the folder as written in
// build.public and the dot names left out.
func publicLeftOut(root string) (public string, left []string, err error) {
	settings, err := codegen.BuildSettings(root)
	if err != nil {
		return "", nil, err
	}
	scan, err := bundle.ScanPublic(filepath.Join(root, settings.Public), settings.PublicDotfiles)
	if err != nil {
		return "", nil, err
	}
	return filepath.ToSlash(filepath.Clean(settings.Public)), scan.LeftOut, nil
}

// checkPublic fails when build.public_dotfiles names a path that public/ does not hold or that
// tifa build would not copy. With hint not nil it writes the line of publicHint to it.
func checkPublic(root string, hint io.Writer) error {
	public, left, err := publicLeftOut(root)
	if err != nil {
		return err
	}
	if line := publicHint(public, left); line != "" && hint != nil {
		fmt.Fprintln(hint, line)
	}
	return nil
}
