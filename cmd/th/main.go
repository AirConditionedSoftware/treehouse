package main

import "github.com/AirConditionedSoftware/treehouse/internal/cmd"

// version is stamped at release time by goreleaser via ldflags.
var version = "dev"

func main() {
	cmd.Execute(version)
}
