package bot

import (
	"strings"
	"testing"
)

func TestExtractTextFromDocument_PlainText(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		mime     string
		data     []byte
		want     string
		wantErr  bool
	}{
		{
			name:     "valid txt file",
			fileName: "hello.txt",
			mime:     "text/plain",
			data:     []byte("hello world\nthis is a test"),
			want:     "hello world\nthis is a test",
			wantErr:  false,
		},
		{
			name:     "valid go file",
			fileName: "main.go",
			mime:     "text/plain",
			data:     []byte("package main\n\nfunc main() {}\n"),
			want:     "package main\n\nfunc main() {}\n",
			wantErr:  false,
		},
		{
			name:     "empty file",
			fileName: "empty.txt",
			mime:     "text/plain",
			data:     []byte(""),
			want:     "",
			wantErr:  true,
		},
		{
			name:     "invalid utf8 / binary",
			fileName: "binary.bin",
			mime:     "application/octet-stream",
			data:     []byte{0x00, 0xFF, 0xFE, 0x00},
			want:     "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractTextFromDocument(tt.fileName, tt.mime, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractTextFromDocument() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractTextFromDocument() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractTextFromDocument_InvalidPDF(t *testing.T) {
	data := []byte("this is not a real pdf but has the extension")
	_, err := extractTextFromDocument("test.pdf", "application/pdf", data)
	if err == nil || !strings.Contains(err.Error(), "failed to parse pdf") {
		t.Errorf("expected pdf parse error, got: %v", err)
	}
}

func TestExtractTextFromDocument_InvalidDocx(t *testing.T) {
	data := []byte("this is not a real docx")
	_, err := extractTextFromDocument("test.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", data)
	if err == nil || !strings.Contains(err.Error(), "failed to parse docx") {
		t.Errorf("expected docx parse error, got: %v", err)
	}
}

func TestExtractTextFromDocument_InvalidXlsx(t *testing.T) {
	data := []byte("this is not a real xlsx")
	_, err := extractTextFromDocument("test.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
	if err == nil || !strings.Contains(err.Error(), "failed to parse excel file") {
		t.Errorf("expected xlsx parse error, got: %v", err)
	}
}
