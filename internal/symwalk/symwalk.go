// Package symwalk walks a directory tree like filepath.WalkDir, but follows symbolic links.
package symwalk

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// Walk calls fn for root and every file and directory under it, in lexical order, as
// filepath.WalkDir does, with the same meaning of fs.SkipDir and fs.SkipAll and the paths
// spelled under root. A link is followed: the entry fn gets is the one of its target, so a link to
// a file is a regular file and a link to a directory is walked. root may be a link. A link whose
// target is missing is an error for fn. A link that leads back to a directory being walked is an
// error that stops the walk, as it would never end.
func Walk(root string, fn fs.WalkDirFunc) error {
	info, err := os.Stat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		var real string
		if real, err = filepath.EvalSymlinks(root); err == nil {
			err = walk(root, fs.FileInfoToDirEntry(info), []string{real}, fn)
		}
	}
	if err == fs.SkipDir || err == fs.SkipAll {
		return nil
	}
	return err
}

// walk visits path, whose entry d is already the target of a link, if it was one. above holds the
// real paths of the directories above path.
func walk(path string, d fs.DirEntry, above []string, fn fs.WalkDirFunc) error {
	err := fn(path, d, nil)
	if err == fs.SkipDir && d.IsDir() {
		return nil
	}
	if err != nil || !d.IsDir() {
		return err
	}
	entries, err := os.ReadDir(path) // sorted by name
	if err != nil {
		if err = fn(path, d, err); err != nil {
			return err
		}
	}
	for _, e := range entries {
		child := filepath.Join(path, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			info, err := os.Stat(child)
			if err != nil {
				if err = fn(child, e, err); err != nil {
					return skipped(err)
				}
				continue
			}
			e = fs.FileInfoToDirEntry(info)
		}
		chain := above
		if e.IsDir() {
			real, err := filepath.EvalSymlinks(child)
			if err != nil {
				return err
			}
			if slices.Contains(above, real) {
				return fmt.Errorf("%s leads back to %s, which is already being walked", child, real)
			}
			chain = append(above[:len(above):len(above)], real)
		}
		if err := walk(child, e, chain, fn); err != nil {
			return skipped(err)
		}
	}
	return nil
}

// skipped ends the loop over the entries of a directory when err is fs.SkipDir, as WalkDir does,
// and returns every other error.
func skipped(err error) error {
	if err == fs.SkipDir {
		return nil
	}
	return err
}
