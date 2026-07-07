package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"log/slog"
)

// runStdio runs the MCP server using the stdio transport. It reads JSON-RPC
// requests from stdin and writes responses to stdout. This is the transport
// used by Cursor and other editors that launch Rexion as a subprocess.
func (s *Server) runStdio(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout

	// Log to stderr so we don't interfere with the JSON-RPC stream on stdout.
	logger := slog.Default()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Some MCP clients send Content-Length headers followed by a blank line
		// and then the JSON body (similar to LSP). Others send newline-delimited
		// JSON. We handle both.
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil // graceful shutdown on stdin close
			}
			return fmt.Errorf("stdio read: %w", err)
		}

		line, isHeader := parseContentLengthHeader(line)
		if isHeader {
			// Content-Length header mode: read the header, then read the body.
			// Skip any remaining header lines until blank line.
			for {
				hdrLine, err := reader.ReadString('\n')
				if err != nil {
					if err == io.EOF {
						return nil
					}
					return fmt.Errorf("stdio read header: %w", err)
				}
				if trimNewline(hdrLine) == "" {
					break
				}
			}
			// The actual JSON message follows.
			msg, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return fmt.Errorf("stdio read body: %w", err)
			}
			line = msg
		}

		line = trimNewline(line)
		if line == "" {
			continue
		}

		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			// Parse error: send a JSON-RPC error response.
			resp := errorResponse(nil, errParseError, "parse error: "+err.Error())
			writeResponse(writer, resp)
			continue
		}

		resp := s.handleRequest(ctx, req)

		// Notifications (no ID) don't get a response.
		if req.IsNotification() {
			logger.Debug("stdio: notification received", "method", req.Method)
			continue
		}

		writeResponse(writer, resp)
	}
}

// parseContentLengthHeader checks if the line is a Content-Length header.
// It returns the remaining content and true if it was a header, or the
// original line and false otherwise.
func parseContentLengthHeader(line string) (string, bool) {
	// Content-Length: N\r\n or Content-Length: N\n
	rest, ok := cutPrefix(line, "Content-Length:")
	if !ok {
		rest, ok = cutPrefix(line, "Content-length:")
	}
	if !ok {
		return line, false
	}
	_ = rest // we don't need to read exactly N bytes; just skip headers
	return "", true
}

// cutPrefix removes a prefix from s and reports whether it was present.
func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}

// trimNewline removes trailing \r and \n characters.
func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// writeResponse writes a JSON-RPC response to the writer as a single line.
func writeResponse(w io.Writer, resp Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		// Last resort: write a minimal error response.
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`+"\n")
		return
	}
	w.Write(data)
	w.Write([]byte("\n"))
}
