package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Separate files let unrelated remotes update independently. The key includes
// the common git directory and URL, so linked worktrees share freshness while
// a replaced remote cannot inherit its predecessor's fetch time. URLs (which
// may include credentials) never appear in the persisted record or filename.
func fetchStampPath(stateDir, commonDir, remote, url string) string {
	key := sha256.Sum256([]byte(commonDir + "\x00" + remote + "\x00" + url))
	return filepath.Join(stateDir, "compare-fetch", fmt.Sprintf("%x.json", key))
}

func recordRemoteFetch(stateDir, commonDir, remote, url string, at time.Time) error {
	if stateDir == "" {
		return nil
	}
	path := fetchStampPath(stateDir, commonDir, remote, url)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(at.UTC())
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".fetch-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func readRemoteFetch(stateDir, commonDir, remote, url string) *time.Time {
	if stateDir == "" {
		return nil
	}
	b, err := os.ReadFile(fetchStampPath(stateDir, commonDir, remote, url))
	if err != nil {
		return nil
	}
	var at time.Time
	if json.Unmarshal(b, &at) != nil || at.IsZero() {
		return nil
	}
	return &at
}

func laterTime(a, b *time.Time) *time.Time {
	if a == nil || b != nil && b.After(*a) {
		return b
	}
	return a
}

// Git's FETCH_HEAD descriptions can omit a local URL's file:// prefix and
// trailing .git. Match only complete source URLs; substring matching could
// falsely freshen a different remote. An ambiguous shared URL names neither;
// explicit o-git fetches still have their own per-remote state record.
func fetchURL(url string) string {
	return strings.TrimSuffix(strings.TrimRight(strings.TrimPrefix(url, "file://"), "/"), ".git")
}

func (r *EndpointResolver) remoteFetchTime(remote string) *time.Time {
	if at, ok := r.remoteFetched[remote]; ok {
		return at
	}
	url := r.remoteURLs[remote]
	at := readRemoteFetch(r.cfg.StateDir, r.commonDir, remote, url)
	matching := 0
	for _, candidate := range r.remoteURLs {
		if fetchURL(candidate) == fetchURL(url) {
			matching++
		}
	}
	if url != "" && matching == 1 {
		if b, err := os.ReadFile(r.fetchHead); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				fields := strings.SplitN(line, "\t", 3)
				if len(fields) != 3 {
					continue
				}
				_, source, ok := strings.Cut(fields[2], " of ")
				if ok && fetchURL(source) == fetchURL(url) {
					if info, err := os.Stat(r.fetchHead); err == nil {
						t := info.ModTime().UTC()
						at = laterTime(at, &t)
					}
					break
				}
			}
		}
	}
	r.remoteFetched[remote] = at
	return at
}
