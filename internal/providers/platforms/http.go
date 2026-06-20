package platforms

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

const MaxAPIErrorBodyBytes = 1024

// ReadAPIError caps remote response bodies so a broken upstream cannot flood
// application logs through a returned error.
func ReadAPIError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, MaxAPIErrorBodyBytes+1))
	truncated := len(body) > MaxAPIErrorBodyBytes
	if truncated {
		body = body[:MaxAPIErrorBodyBytes]
	}
	message := strings.TrimSpace(strings.ToValidUTF8(string(body), "�"))
	if message == "" {
		return fmt.Errorf("API error %d", response.StatusCode)
	}
	if truncated {
		message += "… (truncated)"
	}
	return fmt.Errorf("API error %d: %s", response.StatusCode, message)
}
