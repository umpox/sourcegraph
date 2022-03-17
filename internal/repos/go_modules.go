package repos

import (
	"context"
	"github.com/inconshreveable/log15"
	"github.com/sourcegraph/sourcegraph/internal/extsvc/gomodproxy"
	"github.com/sourcegraph/sourcegraph/internal/httpcli"

	"github.com/sourcegraph/sourcegraph/internal/api"
	dependenciesStore "github.com/sourcegraph/sourcegraph/internal/codeintel/dependencies/store"
	"github.com/sourcegraph/sourcegraph/internal/conf/reposource"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
	"github.com/sourcegraph/sourcegraph/internal/extsvc"
	"github.com/sourcegraph/sourcegraph/internal/jsonc"
	"github.com/sourcegraph/sourcegraph/internal/types"
	"github.com/sourcegraph/sourcegraph/lib/errors"
	"github.com/sourcegraph/sourcegraph/schema"
)

// A GoModulesSource creates git repositories from go module zip files of
// published go dependencies from the Go ecosystem.
type GoModulesSource struct {
	svc       *types.ExternalService
	config    *schema.GoModuleProxiesConnection
	depsStore DependenciesStore
	client    *gomodproxy.Client
}

// NewGoModulesSource returns a new GoModulesSource from the given external service.
func NewGoModulesSource(svc *types.ExternalService, cf *httpcli.Factory) (*GoModulesSource, error) {
	var c schema.GoModuleProxiesConnection
	if err := jsonc.Unmarshal(svc.Config, &c); err != nil {
		return nil, errors.Errorf("external service id=%d config error: %s", svc.ID, err)
	}

	cli, err := cf.Doer()
	if err != nil {
		return nil, err
	}

	return &GoModulesSource{
		svc:    svc,
		config: &c,
		/*dbStore initialized in SetDB */
		client: gomodproxy.NewClient(&c, cli),
	}, nil
}

var _ Source = &GoModulesSource{}

func (s *GoModulesSource) ListRepos(ctx context.Context, results chan SourceResult) {
	deps, err := goDependencies(s.config)
	if err != nil {
		results <- SourceResult{Err: err}
		return
	}

	for _, dep := range deps {
		_, err := s.client.GetVersion(ctx, dep.PackageSyntax(), dep.PackageVersion())
		if err != nil {
			results <- SourceResult{Err: err}
			continue
		}

		repo := s.makeRepo(dep)
		results <- SourceResult{Source: s, Repo: repo}
	}

	lastID := 0
	for {
		depRepos, err := s.depsStore.ListDependencyRepos(ctx, dependenciesStore.ListDependencyReposOpts{
			Scheme:      dependenciesStore.GoModulesScheme,
			After:       lastID,
			Limit:       100,
			NewestFirst: true,
		})
		if err != nil {
			results <- SourceResult{Err: err}
			return
		}
		if len(depRepos) == 0 {
			break
		}

		lastID = depRepos[len(depRepos)-1].ID

		for _, r := range depRepos {
			dep, err := reposource.ParseGoDependency(dbDep.Name)
			if err != nil {
				log15.Error("failed to parse go package name retrieved from database", "package", dbDep.Name, "error", err)
				continue
			}

			goDependency := reposource.GoDependency{GoModule: parsedDbPackage, Version: dbDep.Version}
			pkgKey := goDependency.PackageSyntax()
			info := pkgVersions[pkgKey]

			if info == nil {
				info, err = s.client.GetPackageInfo(ctx, goDependency.GoModule)
				if err != nil {
					pkgVersions[pkgKey] = &gopkg.PackageInfo{Versions: map[string]*gopkg.DependencyInfo{}}
					continue
				}

				pkgVersions[pkgKey] = info
			}

			if _, hasVersion := info.Versions[goDependency.Version]; !hasVersion {
				continue
			}

			repo := s.makeRepo(goDependency.GoModule, info.Description)
			results <- SourceResult{Source: s, Repo: repo}
		}
	}
	log15.Info("finish resolving go artifacts", "totalDB", totalDBFetched, "totalDBResolved", totalDBResolved, "totalConfig", len(goModules))
}

func (s *GoModulesSource) GetRepo(ctx context.Context, name string) (*types.Repo, error) {
	dep, err := reposource.ParseGoDependency(name)
	if err != nil {
		return nil, err
	}

	_, err = s.client.ListVersions(ctx, dep.PackageSyntax())
	if err != nil {
		return nil, err
	}

	return s.makeRepo(dep), nil
}

func (s *GoModulesSource) makeRepo(dep *reposource.GoDependency) *types.Repo {
	urn := s.svc.URN()
	repoName := dep.RepoName()
	return &types.Repo{
		Name: repoName,
		URI:  string(repoName),
		ExternalRepo: api.ExternalRepoSpec{
			ID:          string(repoName),
			ServiceID:   extsvc.TypeGoModules,
			ServiceType: extsvc.TypeGoModules,
		},
		Private: false,
		Sources: map[string]*types.SourceInfo{
			urn: {
				ID:       urn,
				CloneURL: string(repoName),
			},
		},
	}
}

// ExternalServices returns a singleton slice containing the external service.
func (s *GoModulesSource) ExternalServices() types.ExternalServices {
	return types.ExternalServices{s.svc}
}

func (s *GoModulesSource) SetDB(db dbutil.DB) {
	s.depsStore = dependenciesStore.GetStore(database.NewDB(db))
}

func goDependencies(connection *schema.GoModuleProxiesConnection) (dependencies []*reposource.GoDependency, err error) {
	for _, dep := range connection.Dependencies {
		dependency, err := reposource.ParseGoDependency(dep)
		if err != nil {
			return nil, err
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, nil
}
