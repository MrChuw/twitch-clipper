package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"bytes"
	"sync"

)


var clipsPath = "./clips"
var previewsPath = "./previews"

type pCache struct {
	Expiry time.Time
	Body   string
}

var ErrStreamNotFound = errors.New("stream not found")

var playlistCache = map[string]pCache{}

var urlExp = regexp.MustCompile("https?://.+")
var m3SegmentExp = regexp.MustCompile("#EXTINF:.*live\n.+")

var httpClient = &http.Client{Timeout: time.Minute}

func FetchTwitchStream(channelName string) (string, error) {
	d := playlistCache[channelName]

	if time.Now().After(d.Expiry) {
		res, err := httpClient.Get(
			fmt.Sprintf("https://luminous.alienpls.org/live/%s?platform=web&allow_source=true&allow_audio_only=true", url.PathEscape(channelName)),
		)
		if err != nil {
			return "", err
		}

		defer res.Body.Close()
		buf, err := io.ReadAll(res.Body)
		if err != nil {
			return "", err
		}

		d.Body = string(buf)
		if res.StatusCode == http.StatusNotFound {
			return "", ErrStreamNotFound
		} else if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf("proxy -> bad status code (%v):\n%s", res.StatusCode, d.Body)
		}
	}

	streams := urlExp.FindAllString(d.Body, 1)
	if len(streams) == 0 {
		return "", errors.New("no stream playlist available")
	}

	return string(streams[0]), nil

}

func MakeClip(channelName string) ([]byte, error) {
    // 1. Fetch HLS segments
    url, err := FetchTwitchStream(channelName)
    if err != nil {
        return nil, err
    }

    res, err := httpClient.Get(url)
    if err != nil {
        return nil, err
    }

    defer res.Body.Close()
    buf, err := io.ReadAll(res.Body)
    if err != nil {
        return nil, err
    }

    filter := m3SegmentExp.FindAllString(string(buf), -1)
    segments := []string{}
    for _, s := range filter {
        segments = append(segments, s[strings.Index(s, "\n")+1:])
    }

    segmentCount := len(segments)
    buffer := make([][]byte, segmentCount)
    var wg sync.WaitGroup
    var futile bool
    ch := make(chan error, segmentCount)

    // 2. Download segments in parallel
    for i, url := range segments {
        wg.Add(1)
        go func(i int, url string) {
            defer wg.Done()
            res, err := httpClient.Get(url)
            if err != nil && !futile {
                ch <- err
                return
            }
            defer res.Body.Close()
            buf, err := io.ReadAll(res.Body)
            if !futile {
                if err != nil {
                    ch <- err
                    return
                }
                buffer[i] = buf
            }
        }(i, url)
    }

    go func() {
        wg.Wait()
        close(ch)
    }()

    for err := range ch {
        if err != nil {
            futile = true
            return nil, err
        }
    }

    // 3. Pipe between ffmpeg for concatenation and ffmpeg for MP4
    tsToMP4Reader, tsToMP4Writer := io.Pipe()

    // ffmpeg concatenates .ts into a single MPEG-TS stream
    concatCmd := exec.Command("ffmpeg",
        "-hide_banner",
        "-f", "mpegts",
        "-loglevel", "error",
        "-i", "-",
        "-c:v", "copy",
        "-c:a", "copy",
        "-c:s", "copy",
        "-f", "mpegts",
        "pipe:1",
    )
    concatCmd.Stdin = io.NopCloser(bytes.NewReader(bytes.Join(buffer, nil)))
    concatCmd.Stdout = tsToMP4Writer
    concatCmd.Stderr = nil

    // ffmpeg converts MPEG-TS to the final MP4
    var out bytes.Buffer
    var stderr bytes.Buffer
    mp4Cmd := exec.Command("ffmpeg",
        "-hide_banner",
        "-loglevel", "error",
        "-i", "pipe:0",
        "-c:v", "copy",
        "-c:a", "copy",
        "-c:s", "copy",
        "-bsf:a", "aac_adtstoasc",
        "-f", "mp4",
        "-movflags", "frag_keyframe+empty_moov",
        "pipe:1",
    )
    mp4Cmd.Stdin = tsToMP4Reader
    mp4Cmd.Stdout = &out
    mp4Cmd.Stderr = nil

    // 4. Run both ffmpeg processes in parallel
    if err := concatCmd.Start(); err != nil {
        return nil, err
    }
    if err := mp4Cmd.Start(); err != nil {
        return nil, err
    }

    // Wait for them to finish
    concatErr := concatCmd.Wait()
    tsToMP4Writer.Close() // close the pipe
    mp4Err := mp4Cmd.Wait()

    if concatErr != nil {
        return nil, fmt.Errorf("concat ffmpeg failed: %v", concatErr)
    }
    if mp4Err != nil {
        lines := strings.Split(stderr.String(), "\n")
        var filtered []string
        for _, line := range lines {
            line = strings.TrimSpace(line)
            if strings.HasPrefix(line, "Error") || strings.Contains(line, "does not contain any stream") {
                filtered = append(filtered, line)
            }
        }
        filteredMsg := strings.Join(filtered, "\n")
        return nil, fmt.Errorf("ffmpeg failed: %v\n%s", mp4Err, filteredMsg)
    }

    return out.Bytes(), nil
}

func MakePreview(channelName string) ([]byte, error) {
    // First fetch stream segments
    url, err := FetchTwitchStream(channelName)
    if err != nil {
        return nil, err
    }

    // Extract last frame using ffmpeg
    var out bytes.Buffer
    var stderr bytes.Buffer

    cmd := exec.Command("ffmpeg",
        "-hide_banner",
        "-i", url, // Pass the .m3u8 URL here
        "-vframes", "1",   // Get 1 frame
        "-f", "image2",    // Output as an image
        "-q:v", "5",       // Set JPEG quality (2-5 is good)
        "pipe:1",
    )

    cmd.Stdout = &out
    cmd.Stderr = &stderr

    if err := cmd.Run(); err != nil {
        lines := strings.Split(stderr.String(), "\n")
        var filtered []string
        for _, line := range lines {
            line = strings.TrimSpace(line)
            if strings.HasPrefix(line, "Error") || strings.Contains(line, "does not contain any stream") {
                filtered = append(filtered, line)
            }
        }
        filteredMsg := strings.Join(filtered, "\n")
        return nil, fmt.Errorf("ffmpeg failed: %v\n%s", err, filteredMsg)
    }

    return out.Bytes(), nil
}
