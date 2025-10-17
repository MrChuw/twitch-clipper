package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"strconv"
	"time"
	"sync"
	"net"
	"os"
)

var httpAddr = "0.0.0.0:8989"

func resError(w http.ResponseWriter, message string, statusCode int) {
	m := map[string]interface{}{
		"message": message,
		"error":   statusCode,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(m)
}


var (
	visitors   = make(map[string][]time.Time)
	visitorsMu sync.Mutex
	bypassList []string
)

const (
	reqLimit   = 3
	reqWindow  = 15 * time.Second
	cleanupInt = 1 * time.Minute
)

func loadBypassList() {
	raw := os.Getenv("BYPASS_IPS")
	if raw == "" {
		return
	}
	for _, ip := range strings.Split(raw, ",") {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			bypassList = append(bypassList, ip)
		}
	}
}

func isBypassed(ip string) bool {
	for _, b := range bypassList {
		if ip == b {
			return true
		}
	}
	return false
}

func clientKey(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		parts := strings.Split(ip, ",")
		ip = strings.TrimSpace(parts[0])
	} else if val := r.Header.Get("X-Real-IP"); val != "" {
		ip = strings.TrimSpace(val)
	} else {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		} else {
			ip = host
		}
	}
	return ip
}

func allowRequest(ip string) bool {
	if isBypassed(ip) {
		return true
	}

	now := time.Now()
	cutoff := now.Add(-reqWindow)

	visitorsMu.Lock()
	defer visitorsMu.Unlock()

	ts := visitors[ip]
	keep := 0
	for keep < len(ts) && ts[keep].Before(cutoff) {
		keep++
	}
	ts = ts[keep:]
	if len(ts) >= reqLimit {
		visitors[ip] = ts
		return false
	}
	ts = append(ts, now)
	visitors[ip] = ts
	return true
}

func startCleanup() {
	go func() {
		for {
			time.Sleep(cleanupInt)
			cut := time.Now().Add(-reqWindow)
			visitorsMu.Lock()
			for k, ts := range visitors {
				keep := 0
				for keep < len(ts) && ts[keep].Before(cut) {
					keep++
				}
				ts = ts[keep:]
				if len(ts) == 0 {
					delete(visitors, k)
				} else {
					visitors[k] = ts
				}
			}
			visitorsMu.Unlock()
		}
	}()
}

func main() {
	startCleanup()

	http.HandleFunc("/clip/", func(w http.ResponseWriter, r *http.Request) {
		key := clientKey(r)
		if !allowRequest(key) {
			log.Println("rate limit exceeded for:", key)
			resError(w, "rate limit exceeded", 429)
			return
		}

		channelName := strings.ToLower(strings.TrimPrefix(r.URL.Path, "/clip/"))
		log.Printf("Clip requested for: %s", channelName)
		if channelName == "" {
			resError(w, "invalid channel name", 400)
			return
		}

		data, err := MakeClip(channelName)
		if err != nil {
			log.Println("error making clip", err)
			resError(w, err.Error(), 500)
			return
		}

		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data)), 10))
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(data)); err != nil {
			log.Println("error writing response:", err)
		}
	})

	http.HandleFunc("/preview/", func(w http.ResponseWriter, r *http.Request) {
		key := clientKey(r)
		if !allowRequest(key) {
			log.Println("rate limit exceeded for:", key)
			resError(w, "rate limit exceeded", 429)
			return
		}

		channelName := strings.ToLower(strings.TrimPrefix(r.URL.Path, "/preview/"))
		log.Printf("Preview requested for: %s", channelName)
		if channelName == "" {
			resError(w, "invalid channel name", 400)
			return
		}

		data, err := MakePreview(channelName)
		if err != nil {
			log.Println("error making preview", err)
			resError(w, err.Error(), 500)
			return
		}

		w.Header().Set("Content-Type", "image/jpg")
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data)), 10))
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(data)); err != nil {
			log.Println("error writing response:", err)
		}
	})

	log.Println("Server running on " + httpAddr)
	log.Fatal(http.ListenAndServe(httpAddr, nil))
}