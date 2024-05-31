package azurearm

import (
	"os"
	"path/filepath"

	"github.com/zhyocean/trivy/pkg/fanal/analyzer"
	"github.com/zhyocean/trivy/pkg/fanal/analyzer/config"
	"github.com/zhyocean/trivy/pkg/misconf"
)

const (
	version      = 1
	analyzerType = analyzer.TypeAzureARM
)

func init() {
	analyzer.RegisterPostAnalyzer(analyzerType, newAzureARMConfigAnalyzer)
}

// azureARMConfigAnalyzer is an analyzer for detecting misconfigurations in Azure ARM templates.
// It embeds config.Analyzer so it can implement analyzer.PostAnalyzer.
type azureARMConfigAnalyzer struct {
	*config.Analyzer
}

func newAzureARMConfigAnalyzer(opts analyzer.AnalyzerOptions) (analyzer.PostAnalyzer, error) {
	a, err := config.NewAnalyzer(analyzerType, version, misconf.NewAzureARMScanner, opts)
	if err != nil {
		return nil, err
	}
	return &azureARMConfigAnalyzer{Analyzer: a}, nil
}

// Required overrides config.Analyzer.Required() and check if the given file is JSON.
func (a *azureARMConfigAnalyzer) Required(filePath string, _ os.FileInfo) bool {
	return filepath.Ext(filePath) == ".json"
}
