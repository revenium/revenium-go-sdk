package enforcement

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxRulesAnswerBytes bounds the decoded rules answer. The answer carries
// one per-person balance per subscriber who spent this period, about 150
// bytes each, so 64 MiB holds roughly 400,000 people: far past the largest
// team while still refusing a runaway or hostile body.
const maxRulesAnswerBytes = 64 << 20

var errUnreadableRules = errors.New("rules answer could not be read")

func readRuleSet(resp *http.Response, limit int64) (ruleSet, error) {
	body, err := decodedBody(resp)
	if err != nil {
		return ruleSet{}, unreadable(err)
	}
	defer body.Close()
	data, err := readAtMost(body, limit)
	if err != nil {
		return ruleSet{}, unreadable(err)
	}
	set, err := decodeRuleSet(data)
	if err != nil {
		return ruleSet{}, unreadable(err)
	}
	return set, nil
}

func decodedBody(resp *http.Response) (io.ReadCloser, error) {
	if !strings.EqualFold(strings.TrimSpace(resp.Header.Get("Content-Encoding")), "gzip") {
		return io.NopCloser(resp.Body), nil
	}
	reader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("open gzip body: %w", err)
	}
	return reader, nil
}

func readAtMost(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("body is larger than %d bytes", limit)
	}
	return data, nil
}

func unreadable(err error) error {
	return fmt.Errorf("%w: %w", errUnreadableRules, err)
}
