// main.go
//
// Redirect service for p.week-book.ru
//
// Example:
//   https://go.week-book.ru/b1wRAR
// redirects (302) to:
//   https://week-book.ru/posts/indie-web
//
// Post data is fetched live from posts-api on every request
// (GET {API_BASE_URL}/posts/by-short-id/{short_id}), which is an
// authenticated endpoint — requests carry Authorization: Bearer <API_KEY>.
// There is no local cache/store and no more polling of index.json.
//
// Run:
//   go mod init redirect-service
//   go get github.com/go-chi/chi/v5
//   go run main.go
//
// Env:
//   PORT=8080
//   API_BASE_URL=https://api.week-book.ru
//   API_KEY=<posts-api key issued to this consumer>
//   TARGET_BASE=https://week-book.ru/posts
//
// Recommended deploy behind nginx/caddy.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Post mirrors the subset of the posts-api response this service needs.
type Post struct {
	Slug string `json:"slug"`
}

var errNotFound = errors.New("post not found")

type apiClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func newAPIClient(baseURL, apiKey string) *apiClient {
	return &apiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *apiClient) getBySlugShortID(ctx context.Context, shortID string) (Post, error) {
	url := c.baseURL + "/posts/by-short-id/" + shortID

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Post{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return Post{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var post Post
		if err := json.NewDecoder(resp.Body).Decode(&post); err != nil {
			return Post{}, err
		}
		return post, nil
	case http.StatusNotFound:
		return Post{}, errNotFound
	default:
		return Post{}, errors.New("posts-api bad status: " + resp.Status)
	}
}

func main() {
	port := getEnv("PORT", "8080")
	apiBaseURL := getEnv("API_BASE_URL", "https://api.week-book.ru")
	apiKey := os.Getenv("API_KEY")
	targetBase := getEnv("TARGET_BASE", "https://week-book.ru/posts")

	if apiKey == "" {
		log.Fatal("API_KEY is required")
	}

	client := newAPIClient(apiBaseURL, apiKey)

	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	r.Get("/{shortID}", func(w http.ResponseWriter, r *http.Request) {
		shortID := chi.URLParam(r, "shortID")

		post, err := client.getBySlugShortID(r.Context(), shortID)
		if err != nil {
			if errors.Is(err, errNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Println("posts-api error:", err)
			http.Error(w, "upstream error", http.StatusBadGateway)
			return
		}

		target := strings.TrimRight(targetBase, "/") + "/" + post.Slug
		http.Redirect(w, r, target, http.StatusFound)
	})

	log.Println("server started on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func getEnv(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
