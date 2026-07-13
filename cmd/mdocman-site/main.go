package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("SITE_PORT")
	if port == "" {
		port = "8090"
	}
	dir := os.Getenv("SITE_DIR")
	if dir == "" {
		dir = "public-site"
	}
	h := http.FileServer(http.Dir(dir))
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	http.Handle("/", h)
	fmt.Printf("Mdocman 发布端: http://localhost:%s（%s）\n", port, dir)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
