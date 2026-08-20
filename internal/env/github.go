package env

import (
	"context"
	"sort"
)

// Hardcoded runtime versions shown in the register-image picker.
// Refs use docker.imageRepository so they match `make image` (no pull).
var publicRuntimeVersions = []string{
	"0.1.0-rc.7",
	"0.1.0-rc.8",
}

// RemoteCatalog is the hardcoded version list used by the register-image UI.
type RemoteCatalog struct {
	Repo      string        `json:"repo"`
	ImageRepo string        `json:"imageRepo"`
	Releases  []RemoteImage `json:"releases"`
}

// RemoteImage is one runtime that can be registered from the picker.
type RemoteImage struct {
	Version    string `json:"version"`
	Ref        string `json:"ref"`
	Registered bool   `json:"registered"`
	Present    bool   `json:"present"`
}

// ListRemoteImages returns the hardcoded runtime versions.
func (s *Service) ListRemoteImages(ctx context.Context) (RemoteCatalog, error) {
	registered := map[string]string{}
	for _, img := range s.store.imageSnapshot() {
		registered[img.Version] = img.Ref
	}
	repo := s.cfg.Docker.ImageRepository
	out := RemoteCatalog{
		Repo:      repo,
		ImageRepo: repo,
		Releases:  make([]RemoteImage, 0, len(publicRuntimeVersions)),
	}
	for _, ver := range publicRuntimeVersions {
		ref := s.cfg.ImageRef(ver)
		item := RemoteImage{
			Version:    ver,
			Ref:        ref,
			Registered: registered[ver] != "",
		}
		if ok, err := s.drv.ImageExists(ctx, ref); err == nil {
			item.Present = ok
		}
		out.Releases = append(out.Releases, item)
	}
	sort.Slice(out.Releases, func(i, j int) bool {
		if out.Releases[i].Version != out.Releases[j].Version {
			return out.Releases[i].Version > out.Releases[j].Version
		}
		return out.Releases[i].Ref < out.Releases[j].Ref
	})
	return out, nil
}
