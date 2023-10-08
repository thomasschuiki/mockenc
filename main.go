package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type API struct {
	Endpoints []MockEndpoint
}

type MockEndpoint struct {
	Request  MockRequest  `json:"request"`
	Response MockResponse `json:"response"`
}

type MockRequest struct {
	Method   string            `json:"method"`
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers"`
}

type MockResponse struct {
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers"`
	BodyFile string            `json:"bodyFile"`
}

const endpointDir string = "./endpoints"

func main() {
	api := API{}
	err := filepath.WalkDir(endpointDir, walkEndpoints(&api))
	if err != nil {
		fmt.Printf("filepath.WalkDir() returned %v\n", err)
	}
	fmt.Println("endpoints loaded")

  server := &http.Server{Addr: "0.0.0.0:3000", Handler: service(api)}

  serverCtx, serverStopCtx := context.WithCancel(context.Background())

  sig := make(chan os.Signal, 1)
  signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

  timeout := 30*time.Second
  go func ()  {
    <-sig

    // first wait for grace period of 30s
    shutdownCtx, _ := context.WithTimeout(serverCtx, timeout)

    go func() {
      <-shutdownCtx.Done()
      if shutdownCtx.Err() == context.DeadlineExceeded {
        log.Fatalf("gracefull shutdown timed out after %d seconds. forcing shutdown.", timeout)
      }
    }()

    // shutdown for real
    err := server.Shutdown(shutdownCtx)
    if err != nil {
      log.Fatal(err)
    }
    serverStopCtx()

  }()

  err = server.ListenAndServe()
  if err != nil && err != http.ErrServerClosed {
    log.Fatal(err)
  }
  <-serverCtx.Done()
}

func service(api API) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(cors.Handler(cors.Options{
    AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders: []string{"Link"},
	}))
	//r.Use(render.SetContentType(render.ContentTypeJSON))
	for _, endpoint := range api.Endpoints {
		fmt.Printf("registering endpoint: %s %s %s\n", endpoint.Request.Method, endpoint.Request.Endpoint, endpoint.Response.BodyFile)
		r.Method(endpoint.Request.Method, endpoint.Request.Endpoint, writeJsonResponse(endpoint.Response.BodyFile))
	}
  return r
}

func writeJsonResponse(filename string) http.HandlerFunc {
	jsonFile, err := os.ReadFile(filename)
	if err != nil {
		fmt.Println(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write(jsonFile)
	}
}

func walkEndpoints(api *API) fs.WalkDirFunc {
	return func(path string, di fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// skip directories
		if di.IsDir() {
			return nil
		}
		info, _ := di.Info()
		// we are only interested in endpoint files for now
		if strings.HasSuffix(info.Name(), "endpoint.json") {
			jsonFile, err := os.ReadFile(path)
			if err != nil {
				fmt.Println(err)
			}

			e := []MockEndpoint{}
			if err := json.Unmarshal(jsonFile, &e); err != nil {
				fmt.Println("Error unmarshaling", info.Name(), err)
				panic(err)
			}
			for _, ept := range e {
				api.Endpoints = append(api.Endpoints, ept)
			}
			return nil
		}
		return nil
	}
}
