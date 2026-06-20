package bot

import (
	"bytes"
	"encoding/json"
	"strings"
)

const errorDetailLimit = 3000

func errorMessage(err error) string {
	if err == nil {
		return errorStr
	}

	detail := err.Error()
	language := "text"
	heading := ""
	if apiHeading, body, ok := splitAPIError(detail); ok {
		heading = apiHeading + "\n\n"
		detail = body
		language = "json"
	}
	if len(detail) > errorDetailLimit {
		detail = truncateUTF8(detail, errorDetailLimit) + "\n…"
	}

	return errorStr + "\n\n" + heading + fencedCode(language, detail)
}

func splitAPIError(detail string) (heading, body string, ok bool) {
	if !strings.HasPrefix(detail, "API error ") {
		return "", "", false
	}
	separator := strings.Index(detail, ": ")
	if separator < 0 {
		return "", "", false
	}
	rawJSON := detail[separator+2:]
	if !json.Valid([]byte(rawJSON)) {
		return "", "", false
	}

	var formatted bytes.Buffer
	if err := json.Indent(&formatted, []byte(rawJSON), "", "  "); err != nil {
		return "", "", false
	}
	return detail[:separator], formatted.String(), true
}

func fencedCode(language, text string) string {
	fenceLength := 3
	longest := 0
	run := 0
	for index := 0; index < len(text); index++ {
		if text[index] == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	if longest >= fenceLength {
		fenceLength = longest + 1
	}

	return fencedCodeWithLength(language, text, fenceLength)
}

func fencedCodeWithLength(language, text string, fenceLength int) string {
	fence := strings.Repeat("`", fenceLength)
	return fence + language + "\n" + text + "\n" + fence
}
