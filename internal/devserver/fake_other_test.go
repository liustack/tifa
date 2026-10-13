//go:build !unix

package devserver

import "errors"

func startDetachedChild() error { return errors.New("detached children need a unix system") }

func watchWithFileLimit() {}
