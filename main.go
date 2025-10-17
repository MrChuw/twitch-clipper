package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"strconv"
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

func main() {
	http.HandleFunc("/clip/", func(w http.ResponseWriter, r *http.Request) {
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

		if _, err := w.Write(data); err != nil {
			log.Println("error writing response:", err)
		}
	})

	http.HandleFunc("/preview/", func(w http.ResponseWriter, r *http.Request) {
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

		if _, err := w.Write(data); err != nil {
			log.Println("error writing response:", err)
		}
	})

	log.Println("Server running on " + httpAddr)

	log.Fatal(http.ListenAndServe(httpAddr, nil))
}
