// cmd/server is the Go rewrite's application server: it hosts the Modbus
// slave simulation engine behind a JSON API (internal/api) and serves the
// compiled React frontend from web/dist. The tick loops for data sources and
// point mutation run for the process lifetime.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Karl-Dai/ModbusSim/go/internal/api"
	"github.com/Karl-Dai/ModbusSim/go/internal/app"
)

func main() {
	defaultAddr := "127.0.0.1:8355"
	if v := os.Getenv("MODBUSSIM_ADDR"); v != "" {
		defaultAddr = v
	}
	addr := flag.String("addr", defaultAddr, "listen address")
	webDir := flag.String("web", envOr("MODBUSSIM_WEB_DIR", "web/dist"), "static frontend directory")
	flag.Parse()

	state := app.NewState()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state.StartDataSourceLoop(ctx)
	state.StartMutationLoop(ctx)

	mux := http.NewServeMux()
	api.NewServer(state).Routes(mux)

	// Static frontend (built by npm run build in go/web), SPA fallback to
	// index.html for client-side routes.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean != "" {
			if _, err := os.Stat(*webDir + "/" + clean); err == nil {
				http.ServeFile(w, r, *webDir+"/"+clean)
				return
			}
		}
		http.ServeFile(w, r, *webDir+"/index.html")
	})

	log.Printf("ModbusSim (Go) listening on http://%s (web dir: %s)", *addr, *webDir)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
