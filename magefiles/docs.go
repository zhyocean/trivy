//go:build mage_docs

package main

import (
	"github.com/spf13/cobra/doc"

	"github.com/zhanglimao/trivy/pkg/commands"
	"github.com/zhanglimao/trivy/pkg/flag"
	"github.com/zhanglimao/trivy/pkg/log"
)

// Generate CLI references
func main() {
	ver, err := version()
	if err != nil {
		log.Fatal(err)
	}
	// Set a dummy path for the documents
	flag.CacheDirFlag.Value = "/path/to/cache"
	flag.ModuleDirFlag.Value = "$HOME/.trivy/modules"

	cmd := commands.NewApp(ver)
	cmd.DisableAutoGenTag = true
	if err = doc.GenMarkdownTree(cmd, "./docs/docs/references/configuration/cli"); err != nil {
		log.Fatal(err)
	}
}
