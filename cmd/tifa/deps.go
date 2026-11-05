package main

// The generated main imports tifa/server. An application lists this command as a tool in its
// go.mod (go get -tool github.com/liustack/tifa/cmd/tifa), and go mod tidy keeps every module a
// tool needs, though it skips .tifa. Importing server here keeps its dependencies, quickjs among
// them, in the application's go.mod and go.sum, so a tidy module builds without changes.
import _ "github.com/liustack/tifa/server"
