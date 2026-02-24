package main

import (
	"log"
	"net/http"
	"os"

	"github.com/BRO3886/go-docpdf/internal/converter"
	"github.com/BRO3886/go-docpdf/internal/handler"
)

func main() {
	conv := converter.New()
	convertHandler := handler.NewConvert(conv)

	mux := http.NewServeMux()
	mux.Handle("/convert", convertHandler)
	mux.HandleFunc("/health", handler.Health)

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	log.Printf("starting server on %s (libreoffice binary: %s)", addr, conv.BinaryPath)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
