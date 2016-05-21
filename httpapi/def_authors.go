package httpapi

import (
	"net/http"

	"github.com/gorilla/mux"
	"sourcegraph.com/sourcegraph/sourcegraph/go-sourcegraph/sourcegraph"
	"sourcegraph.com/sourcegraph/sourcegraph/pkg/routevar"
	"sourcegraph.com/sourcegraph/sourcegraph/util/handlerutil"
)

func serveDefAuthors(w http.ResponseWriter, r *http.Request) error {
	ctx, cl := handlerutil.Client(r)

	var opt sourcegraph.DefListAuthorsOptions
	if err := schemaDecoder.Decode(&opt, r.URL.Query()); err != nil {
		return err
	}

	defSpec, err := routevar.ToDefSpec(mux.Vars(r))
	if err != nil {
		return err
	}

	authors, err := cl.Defs.ListAuthors(ctx, &sourcegraph.DefsListAuthorsOp{
		Def: defSpec,
		Opt: &opt,
	})
	if err != nil {
		return err
	}

	return writeJSON(w, authors)
}
