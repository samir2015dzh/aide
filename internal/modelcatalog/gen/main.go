// Command gen regenerates the model catalog's built-in snapshot from the live
// models.dev and LiteLLM catalogs, with the same parsing and merge rules the
// daemon's background sync uses. Run it from the module root:
//
//	go run ./internal/modelcatalog/gen
//
// -models-dev-file and -litellm-file read saved copies instead of fetching.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/samir2015dzh/aide/internal/modelcatalog"
)

func main() {
	out := flag.String("o", "internal/modelcatalog/snapshot.json", "snapshot file to write")
	mdURL := flag.String("models-dev", modelcatalog.DefaultModelsDevURL, "models.dev api.json URL")
	llURL := flag.String("litellm", modelcatalog.DefaultLiteLLMURL, "LiteLLM model_prices_and_context_window.json URL")
	mdFile := flag.String("models-dev-file", "", "read models.dev data from this file instead of -models-dev")
	llFile := flag.String("litellm-file", "", "read LiteLLM data from this file instead of -litellm")
	flag.Parse()

	md, err := load(*mdFile, *mdURL)
	if err != nil {
		fail("models.dev: %v", err)
	}
	ll, err := load(*llFile, *llURL)
	if err != nil {
		fail("litellm: %v", err)
	}
	data, missing, err := modelcatalog.BuildSnapshot(md, ll, time.Now().UTC().Truncate(time.Second))
	if err != nil {
		fail("build: %v", err)
	}
	for _, id := range missing {
		fmt.Fprintf(os.Stderr, "gen: %s is not in either upstream catalog; left out\n", id)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fail("%v", err)
	}
	fmt.Fprintf(os.Stderr, "gen: wrote %s\n", *out)
}

func load(file, url string) ([]byte, error) {
	if file != "" {
		return os.ReadFile(file)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen: "+format+"\n", args...)
	os.Exit(1)
}
