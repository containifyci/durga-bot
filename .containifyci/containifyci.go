//go:generate sh -c "if [ ! -f go.mod ]; then echo 'Initializing go.mod...'; go mod init .containifyci; else echo 'go.mod already exists. Skipping initialization.'; fi"
//go:generate go get github.com/containifyci/engine-ci/protos2
//go:generate go get github.com/containifyci/engine-ci/client
//go:generate go mod tidy

package main

import (
	"os"

	"github.com/containifyci/engine-ci/client/pkg/build"
	"github.com/containifyci/engine-ci/protos2"
)

func main() {
	os.Chdir("../")

	// Build Group 0
	durgabot := build.NewGoLibraryBuild("durga-bot")
	durgabot.Folder = "."
	durgabot.File = "cmd/durga-bot/main.go"

	//TODO: adjust the registries to your own container registry
	build.BuildGroups(
		&protos2.BuildArgsGroup{
			Args: []*protos2.BuildArgs{
				durgabot,
			},
		},
	)
}
