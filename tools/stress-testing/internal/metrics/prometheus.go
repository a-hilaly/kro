package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) QueryNumber(ctx context.Context, promQL string, ts time.Time) (float64, error) {
	queryURL, err := url.Parse(c.baseURL + "/api/v1/query")
	if err != nil {
		return 0, fmt.Errorf("build query URL: %w", err)
	}

	values := queryURL.Query()
	values.Set("query", promQL)
	if !ts.IsZero() {
		values.Set("time", ts.Format(time.RFC3339Nano))
	}
	queryURL.RawQuery = values.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("query prometheus: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return 0, fmt.Errorf("prometheus returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload queryResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode prometheus response: %w", err)
	}
	if payload.Status != "success" {
		return 0, fmt.Errorf("prometheus query failed: %s", payload.Error)
	}

	return payload.Data.sum()
}

type queryResponse struct {
	Status    string    `json:"status"`
	ErrorType string    `json:"errorType"`
	Error     string    `json:"error"`
	Data      queryData `json:"data"`
}

type queryData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

func (d queryData) sum() (float64, error) {
	switch d.ResultType {
	case "vector":
		var result []vectorResult
		if err := json.Unmarshal(d.Result, &result); err != nil {
			return 0, err
		}

		var total float64
		for _, item := range result {
			value, err := parseSampleValue(item.Value)
			if err != nil {
				return 0, err
			}
			total += value
		}
		return total, nil
	case "scalar":
		var value []interface{}
		if err := json.Unmarshal(d.Result, &value); err != nil {
			return 0, err
		}
		return parseSampleValue(value)
	default:
		return 0, fmt.Errorf("unsupported result type %q", d.ResultType)
	}
}

type vectorResult struct {
	Value []interface{} `json:"value"`
}

func parseSampleValue(value []interface{}) (float64, error) {
	if len(value) != 2 {
		return 0, fmt.Errorf("unexpected sample shape: %#v", value)
	}

	raw, ok := value[1].(string)
	if !ok {
		return 0, fmt.Errorf("unexpected sample value type %T", value[1])
	}

	return strconv.ParseFloat(raw, 64)
}
