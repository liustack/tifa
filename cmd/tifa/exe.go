package main

import "runtime"

// exeSuffix ends the name of a program that runs on this system: Windows runs only files named
// .exe. It is for programs tifa builds to run here, such as the one tifa dev runs. A build for
// another system follows GOOS instead, see defaultOutput.
var exeSuffix = func() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}()

// runningExeLocked says that the system refuses to write over or delete a program that runs, as
// Windows does. It lets one be renamed, see moveAside.
const runningExeLocked = runtime.GOOS == "windows"
