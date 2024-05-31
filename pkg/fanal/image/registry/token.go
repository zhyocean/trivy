package registry

import (
	"context"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/zhyocean/trivy/pkg/fanal/image/registry/azure"
	"github.com/zhyocean/trivy/pkg/fanal/image/registry/ecr"
	"github.com/zhyocean/trivy/pkg/fanal/image/registry/google"
	"github.com/zhyocean/trivy/pkg/fanal/log"
	"github.com/zhyocean/trivy/pkg/fanal/types"
)

var (
	registries []Registry
)

func init() {
	RegisterRegistry(&google.Registry{})
	RegisterRegistry(&ecr.ECR{})
	RegisterRegistry(&azure.Registry{})
}

type Registry interface {
	CheckOptions(domain string, option types.RegistryOptions) error
	GetCredential(ctx context.Context) (string, string, error)
}

func RegisterRegistry(registry Registry) {
	registries = append(registries, registry)
}

func GetToken(ctx context.Context, domain string, opt types.RegistryOptions) (auth authn.Basic) {
	// check registry which particular to get credential
	for _, registry := range registries {
		err := registry.CheckOptions(domain, opt)
		if err != nil {
			continue
		}
		username, password, err := registry.GetCredential(ctx)
		if err != nil {
			// only skip check registry if error occurred
			log.Logger.Debug(err)
			break
		}
		return authn.Basic{
			Username: username,
			Password: password,
		}
	}
	return authn.Basic{}
}
