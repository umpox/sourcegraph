package reposource

import (
	"strings"

	"golang.org/x/mod/module"

	"github.com/sourcegraph/sourcegraph/internal/api"
)

// GoDependency is a go module at a specific version.
type GoDependency struct {
	mod module.Version
}

// ParseGoDependency parses a string in a 'module@version' format into a GoDependency.
func ParseGoDependency(dependency string) (*GoDependency, error) {
	var mod module.Version

	if i := strings.LastIndex(dependency, "@"); i == -1 {
		mod.Path = dependency
		mod.Version = "latest"
	} else {
		mod.Path = dependency[:i]
		mod.Version = dependency[i+1:]
	}

	err := module.Check(mod.Path, mod.Version)
	if err != nil {
		return nil, err
	}

	return &GoDependency{mod: mod}, nil
}

// RepoName provides a name that is "globally unique" for a Sourcegraph instance.
//
// The returned value is used for repo:... in queries.
func (m *GoDependency) RepoName() api.RepoName {
	return api.RepoName("go/" + m.mod.Path)
}

// PackageSyntax returns the module name.
func (m *GoDependency) PackageSyntax() string {
	return m.mod.Path
}

// PackageManagerSyntax returns the module name and version in mod@version format.
func (m *GoDependency) PackageManagerSyntax() string {
	return m.mod.String()
}

func (m *GoDependency) Scheme() string {
	return "go"
}

func (m *GoDependency) PackageVersion() string {
	return m.mod.Version
}

func (m *GoDependency) GitTagFromVersion() string {
	return "v" + m.mod.Version
}

func (d *GoDependency) Equal(other *GoDependency) bool {
	return d == other || (d != nil && other != nil && d.mod == other.mod)
}
