package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/BRO3886/go-docpdf/internal/converter"
	"github.com/BRO3886/go-docpdf/internal/handler"
	"github.com/BRO3886/go-docpdf/internal/metrics"
	"github.com/BRO3886/go-docpdf/internal/middleware"
)

func main() {
	conv := converter.New()
	reg := metrics.New()
	convertHandler := handler.NewConvert(conv)

	mux := http.NewServeMux()
	mux.Handle("/convert", middleware.Metrics(reg, convertHandler))
	mux.HandleFunc("/health", handler.Health)
	mux.Handle("/metrics", reg)

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	startMsg, _ := json.Marshal(map[string]any{
		"time":    time.Now().UTC().Format(time.RFC3339),
		"level":   "info",
		"msg":     "starting server",
		"addr":    addr,
		"soffice": conv.BinaryPath,
	})
	fmt.Fprintf(os.Stderr, "%s\n", startMsg)

	chain := middleware.RequestID(middleware.Logging(mux))
	if err := http.ListenAndServe(addr, chain); err != nil {
		errMsg, _ := json.Marshal(map[string]any{
			"time":  time.Now().UTC().Format(time.RFC3339),
			"level": "fatal",
			"msg":   "server error",
			"error": err.Error(),
		})
		fmt.Fprintf(os.Stderr, "%s\n", errMsg)
		os.Exit(1)
	}
}
