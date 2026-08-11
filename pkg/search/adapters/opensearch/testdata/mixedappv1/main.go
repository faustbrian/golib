package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"
)

const maximumResponseBytes = 1 << 20

var safeIndexName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,254}$`)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	endpoint, alias, physical := os.Getenv("OPENSEARCH_URL"), os.Getenv("OPENSEARCH_MIXED_ALIAS"), os.Getenv("OPENSEARCH_MIXED_PHYSICAL")
	barrier := os.Getenv("OPENSEARCH_MIXED_BARRIER")
	count, err := strconv.Atoi(os.Getenv("OPENSEARCH_MIXED_DOCUMENTS"))
	if err != nil || count <= 0 || count > 64 || !safeIndexName.MatchString(alias) || !safeIndexName.MatchString(physical) {
		return errors.New("mixed application v1 configuration is invalid")
	}
	base, err := url.Parse(endpoint)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return errors.New("mixed application v1 endpoint is invalid")
	}
	barrierURL, err := url.Parse(barrier)
	if err != nil || barrierURL.Scheme != "http" || barrierURL.Host == "" {
		return errors.New("mixed application v1 barrier is invalid")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	barrierRequest, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, barrierURL.String(), nil)
	barrierResponse, err := client.Do(barrierRequest)
	if err != nil {
		return errors.New("mixed application v1 barrier failed")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(barrierResponse.Body, maximumResponseBytes+1))
	closeErr := barrierResponse.Body.Close()
	if closeErr != nil || barrierResponse.StatusCode != http.StatusNoContent {
		return errors.New("mixed application v1 barrier was rejected")
	}

	for position := range count {
		id := fmt.Sprintf("application-v1-%03d", position)
		writeURL := *base
		writeURL.Path = "/" + alias + "/_doc/" + id
		query := writeURL.Query()
		query.Set("version", "1")
		query.Set("version_type", "external")
		query.Set("refresh", "wait_for")
		query.Set("require_alias", "true")
		writeURL.RawQuery = query.Encode()
		body := []byte(fmt.Sprintf(`{"writer":"application-v1","sequence":%d}`, position))
		response, status, requestErr := requestJSON(client, http.MethodPut, writeURL.String(), body)
		if requestErr != nil || status != http.StatusCreated && status != http.StatusOK {
			return errors.New("mixed application v1 write failed")
		}
		var acknowledged struct {
			Index   string `json:"_index"`
			ID      string `json:"_id"`
			Version uint64 `json:"_version"`
			Result  string `json:"result"`
		}
		if decodeOne(response, &acknowledged) != nil || acknowledged.Index != physical || acknowledged.ID != id || acknowledged.Version != 1 ||
			acknowledged.Result != "created" && acknowledged.Result != "updated" {
			return errors.New("mixed application v1 write attribution failed")
		}
		if position%4 == 3 {
			searchURL := *base
			searchURL.Path = "/" + alias + "/_search"
			searchBody := []byte(`{"query":{"term":{"writer":"application-v1"}},"size":64,"version":true,"sort":[{"_id":{"order":"asc"}}]}`)
			searchResponse, searchStatus, searchErr := requestJSON(client, http.MethodPost, searchURL.String(), searchBody)
			if searchErr != nil || searchStatus != http.StatusOK || !validSearchResponse(searchResponse, physical, position+1) {
				return errors.New("mixed application v1 search failed")
			}
		}
	}
	return nil
}

func requestJSON(client *http.Client, method, target string, body []byte) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(context.Background(), method, target, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	if response == nil || response.Body == nil {
		return nil, 0, errors.New("response is missing")
	}
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(payload) > maximumResponseBytes {
		return nil, response.StatusCode, errors.New("response exceeds its bound")
	}
	return payload, response.StatusCode, nil
}

func validSearchResponse(body []byte, physical string, expected int) bool {
	var response struct {
		TimedOut bool `json:"timed_out"`
		Shards   struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
		Hits struct {
			Total struct {
				Value uint64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				Index   string `json:"_index"`
				ID      string `json:"_id"`
				Version uint64 `json:"_version"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if decodeOne(body, &response) != nil || response.TimedOut || response.Shards.Failed != 0 || response.Hits.Total.Value != uint64(expected) || len(response.Hits.Hits) != expected {
		return false
	}
	for position, hit := range response.Hits.Hits {
		if hit.Index != physical || hit.ID != fmt.Sprintf("application-v1-%03d", position) || hit.Version != 1 {
			return false
		}
	}
	return true
}

func decodeOne(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("response contains trailing data")
	}
	return nil
}
