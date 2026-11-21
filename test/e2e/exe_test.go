package e2e

import "runtime"

// exeSuffix ends the name of a program that runs on this system: Windows runs only files named .exe.
var exeSuffix = func() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}()
