package local

import (
	"context"
	"github.com/zhanglimao/trivy/pkg/fanal/artifact"
	"github.com/zhanglimao/trivy/pkg/fanal/cache"
	"github.com/zhanglimao/trivy/pkg/fanal/types"
)

type Packet struct {
}

func NewPacketArtifact(rootPath string, c cache.ArtifactCache, opt artifact.Option) (artifact.Artifact, error) {

	return Packet{}, nil
}

func (a Packet) Inspect(ctx context.Context) (types.ArtifactReference, error) {
	return types.ArtifactReference{}, nil
}

func (a Packet) Clean(reference types.ArtifactReference) error {
	return nil
}
