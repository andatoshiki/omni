package bot

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/nguyenthenguyen/docx"
	"github.com/xuri/excelize/v2"
)

var xmlTagRegex = regexp.MustCompile(`<[^>]+>`)

// extractTextFromDocument attempts to extract plain text from common document formats.
func extractTextFromDocument(fileName string, mimeType string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("document is empty")
	}

	lowerFileName := strings.ToLower(fileName)

	// PDF Parsing
	if strings.HasSuffix(lowerFileName, ".pdf") || mimeType == "application/pdf" {
		reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("failed to parse pdf: %w", err)
		}
		var buf bytes.Buffer
		b, err := reader.GetPlainText()
		if err != nil {
			return "", fmt.Errorf("failed to read pdf text: %w", err)
		}
		if _, err := io.Copy(&buf, b); err != nil {
			return "", fmt.Errorf("failed to copy pdf text: %w", err)
		}
		text := strings.TrimSpace(buf.String())
		if text == "" {
			return "", fmt.Errorf("pdf contains no extractable text")
		}
		return text, nil
	}

	// DOCX Parsing
	if strings.HasSuffix(lowerFileName, ".docx") || mimeType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		doc, err := docx.ReadDocxFromMemory(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("failed to parse docx: %w", err)
		}
		defer doc.Close()
		content := doc.Editable().GetContent()
		// Replace common DOCX paragraph/break tags with newlines before stripping XML
		content = strings.ReplaceAll(content, "</w:p>", "\n")
		content = strings.ReplaceAll(content, "<w:br/>", "\n")
		content = strings.ReplaceAll(content, "<w:tab/>", "\t")
		text := xmlTagRegex.ReplaceAllString(content, "")
		text = strings.TrimSpace(text)
		if text == "" {
			return "", fmt.Errorf("docx contains no extractable text")
		}
		return text, nil
	}

	// XLSX Parsing
	if strings.HasSuffix(lowerFileName, ".xlsx") || mimeType == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			return "", fmt.Errorf("failed to parse excel file: %w", err)
		}
		defer f.Close()

		var buf bytes.Buffer
		for _, sheetName := range f.GetSheetList() {
			rows, err := f.GetRows(sheetName)
			if err != nil {
				continue
			}
			buf.WriteString(fmt.Sprintf("--- Sheet: %s ---\n", sheetName))
			for _, row := range rows {
				buf.WriteString(strings.Join(row, "\t") + "\n")
			}
			buf.WriteString("\n")
		}
		text := strings.TrimSpace(buf.String())
		if text == "" {
			return "", fmt.Errorf("excel file contains no data")
		}
		return text, nil
	}

	// Plain text & Code files
	// Read up to 1KB to check if it's UTF-8 without null bytes
	checkLen := len(data)
	if checkLen > 1024 {
		checkLen = 1024
	}
	sample := data[:checkLen]
	if !bytes.Contains(sample, []byte{0}) {
		// Clean the text of any invalid UTF-8 sequences and return
		return strings.ToValidUTF8(string(data), ""), nil
	}

	return "", fmt.Errorf("unsupported document type or binary format")
}
