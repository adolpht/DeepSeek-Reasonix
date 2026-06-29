package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWritePdf(t *testing.T) {
	tmpDir := t.TempDir()
	testPdf := filepath.Join(tmpDir, "test.pdf")

	tool := writePdf{roots: []string{tmpDir}, workDir: tmpDir}

	// Test basic PDF generation
	args := map[string]interface{}{
		"path":    testPdf,
		"content": "# Title\n\nThis is a paragraph.\n\n## Section\n\nAnother paragraph.",
		"title":   "Test Document",
	}

	rawArgs, _ := json.Marshal(args)
	result, err := tool.Execute(context.Background(), rawArgs)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result == "" {
		t.Error("Expected non-empty result")
	}

	// Verify file was created
	if _, err := os.Stat(testPdf); os.IsNotExist(err) {
		t.Error("PDF file was not created")
	}

	// Verify file has content
	info, _ := os.Stat(testPdf)
	if info.Size() == 0 {
		t.Error("PDF file is empty")
	}
}

func TestWritePdfWithTable(t *testing.T) {
	tmpDir := t.TempDir()
	testPdf := filepath.Join(tmpDir, "table.pdf")

	tool := writePdf{roots: []string{tmpDir}, workDir: tmpDir}

	content := `# Table Test

| Column 1 | Column 2 | Column 3 |
|----------|----------|----------|
| Cell 1   | Cell 2   | Cell 3   |
| Cell 4   | Cell 5   | Cell 6   |

End of document.`

	args := map[string]interface{}{
		"path":    testPdf,
		"content": content,
	}

	rawArgs, _ := json.Marshal(args)
	result, err := tool.Execute(context.Background(), rawArgs)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result == "" {
		t.Error("Expected non-empty result")
	}

	// Verify file was created
	if _, err := os.Stat(testPdf); os.IsNotExist(err) {
		t.Error("PDF file was not created")
	}
}

func TestWritePdfConfine(t *testing.T) {
	workspace := t.TempDir()
	otherDir := t.TempDir()
	outsidePath := filepath.Join(otherDir, "test.pdf")

	tool := writePdf{roots: []string{workspace}, workDir: workspace}

	args := map[string]interface{}{
		"path":    outsidePath,
		"content": "# Test",
	}

	rawArgs, _ := json.Marshal(args)
	_, err := tool.Execute(context.Background(), rawArgs)

	// Should fail because path is outside workspace
	if err == nil {
		t.Error("Expected error for path outside workspace")
	}
}
