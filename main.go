// main.go
//
// Redirect service for p.week-book.ru
//
// Example:
//   https://p.week-book.ru/b1wRAR
// redirects to:
//   https://week-book.ru/posts/indie-web
//
// Run:
//   go mod init redirect-service
//   go get github.com/go-chi/chi/v5
//   go run main.go
//
// Env:
//   PORT=8080
//   JSON_URL=https://s3.week-book.ru/posts/index.json
//   TARGET_BASE=https://week-book.ru/posts
//
// Recommended deploy behind nginx/caddy.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type PostMeta struct {
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
}

type Post struct {
	ShortID  string   `json:"short_id"`
	Slug     string   `json:"slug"`
	Filename string   `json:"filename"`
	Meta     PostMeta `json:"meta"`
}

type Store struct {
	mu   sync.RWMutex
	data map[string]Post
}

func NewStore() *Store {
	return &Store{data: make(map[string]Post)}
}

func (s *Store) SetAll(items map[string]Post) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = items
}

func (s *Store) Get(shortID string) (Post, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.data[shortID]
	return p, ok
}

func main() {
	port := getEnv("PORT", "8080")
	jsonURL := getEnv("JSON_URL", "https://s3.week-book.ru/posts/index.json")
	targetBase := getEnv("TARGET_BASE", "https://week-book.ru/posts")

	store := NewStore()

	// initial load
	if err := refresh(store, jsonURL); err != nil {
		log.Fatal(err)
	}

	// background refresh every 5 min
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			if err := refresh(store, jsonURL); err != nil {
				log.Println("refresh error:", err)
			}
		}
	}()

	r := chi.NewRouter()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	r.Get("/{shortID}", func(w http.ResponseWriter, r *http.Request) {
		shortID := chi.URLParam(r, "shortID")
		post, ok := store.Get(shortID)
		if !ok {
			http.NotFound(w, r)
			return
		}

		target := strings.TrimRight(targetBase, "/") + "/" + post.Slug

		title := html.EscapeString(post.Meta.Title)
		desc := html.EscapeString(post.Meta.Excerpt)
		url := html.EscapeString(target)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <title>%s</title>
  <meta name="description" content="%s">
  <meta property="og:title" content="%s — Week-book">
  <meta property="og:description" content="%s">
  <meta property="og:url" content="%s">
  <meta property="og:type" content="article">
  <meta http-equiv="refresh" content="0; url=%s">
  <link rel="canonical" href="%s">
</head>
<body>
  <p>Перенаправление… <a href="%s">%s</a></p>
</body>
</html>`, title, desc, title, desc, url, url, url, url, title)
	})
	log.Println("server started on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func refresh(store *Store, url string) error {
	posts, err := fetchPosts(url)
	if err != nil {
		return err
	}
	tmp := make(map[string]Post, len(posts))
	for _, p := range posts {
		if p.ShortID == "" || p.Slug == "" {
			continue
		}
		tmp[p.ShortID] = p
	}
	store.SetAll(tmp)
	log.Println("loaded redirects:", len(tmp))
	return nil
}

func fetchPosts(url string) ([]Post, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
		return nil, errors.New("bad status: " + resp.Status)
	}

	var posts []Post
	if err := json.NewDecoder(resp.Body).Decode(&posts); err != nil {
		return nil, err
	}

	return posts, nil
}

func getEnv(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
