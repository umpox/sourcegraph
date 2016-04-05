// +build exectest

package testserver

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/gorp.v1"

	"sourcegraph.com/sourcegraph/sourcegraph/server/internal/localstore"
	"sourcegraph.com/sourcegraph/sourcegraph/util/dbutil2"
	"sourcegraph.com/sourcegraph/sourcegraph/util/testdb"
)

// dbConfig is embedded in TestServer.
type dbConfig struct {
	MainDBH gorp.SqlExecutor
	dbDone  func()
}

func (s *dbConfig) configDB() error {
	s.MainDBH, s.dbDone = testdb.NewHandle(&localstore.Schema)
	if _, ok := s.MainDBH.(*dbutil2.Handle); !ok {
		return fmt.Errorf("test app requires a real main *dbutil.Handle not %T (must run with -pgsqltest.init=full)", s.MainDBH)
	}
	return nil
}

func (s *dbConfig) dbEnvConfig() []string {
	parseDBName := func(s string) string {
		fs := strings.Fields(s)
		for _, f := range fs {
			if strings.HasPrefix(f, "dbname=") {
				return strings.TrimPrefix(f, "dbname=")
			}
		}
		panic("no dbname= found in data source: '" + s + "'")
	}
	v := []string{"PGDATABASE=" + parseDBName(s.MainDBH.(*dbutil2.Handle).DataSource)}
	v = append(v, "PGSSLMODE=disable")
	if u := os.Getenv("PGUSER"); u != "" {
		v = append(v, "PGUSER="+u)
	}
	return v
}

func (s *dbConfig) close() { s.dbDone() }
