// Command client serves a small web page that plays tic-tac-toe against the
// server. The page is embedded in the binary, so the client is self-contained.
package main

import (
	_ "embed"
	"flag"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
)

//go:embed index.html
var indexHTML string

func main() {
	addr := flag.String("addr", ":3000", "address to serve the page on")
	server := flag.String("server", "ws://localhost:8080/ws", "game server websocket URL")
	flag.Parse()

	// The URL is written into the page as a JavaScript string; html/template
	// escapes it for that context.
	page := template.Must(template.New("index").Parse(indexHTML))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := page.Execute(w, *server); err != nil {
			log.Printf("render page: %v", err)
		}
	})

	host := *addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	log.Printf("open http://%s to play (server %s)", host, *server)
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
