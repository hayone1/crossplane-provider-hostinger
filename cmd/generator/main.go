package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crossplane/upjet/v2/pkg/pipeline"

	"github.com/hayone1/crossplane-provider-hostinger/config"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "" {
		panic("root directory is required to be given as argument")
	}
	rootDir := os.Args[1]
	absRootDir, err := filepath.Abs(rootDir)
	if err != nil {
		panic(fmt.Sprintf("cannot calculate the absolute path with %s", rootDir))
	}
	pc, err := config.GetProvider(true)
	if err != nil {
		panic(fmt.Sprintf("cannot get the cluster-scoped provider configuration: %v", err))
	}
	pn, err := config.GetProviderNamespaced(true)
	if err != nil {
		panic(fmt.Sprintf("cannot get the namespaced provider configuration: %v", err))
	}
	pipeline.Run(pc, pn, absRootDir)
}
