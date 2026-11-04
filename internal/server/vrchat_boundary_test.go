package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Keep VRChat's HTTP and WebSocket protocols inside the SDK. The server also
// calls Turnstile, and the desktop app also contacts VRCove and GitHub.
func TestVRChatRequestsUseSDK(t *testing.T) {
	for _, root := range []string{".", filepath.Join("..", "..", "companion")} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				switch entry.Name() {
				case "frontend", "dist", "node_modules", "build":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			for _, imp := range parsed.Imports {
				value, _ := strconv.Unquote(imp.Path.Value)
				// The relay accepts the browser's WebSocket and only copies
				// encrypted bytes. It never speaks VRChat's protocol.
				if filepath.Base(path) == "relay.go" && filepath.Clean(root) == "." {
					continue
				}
				if value == "github.com/coder/websocket" || value == "github.com/gorilla/websocket" {
					t.Errorf("%s imports a WebSocket client outside the SDK", path)
				}
			}
			server := filepath.Clean(root) == "."
			ast.Inspect(parsed, func(node ast.Node) bool {
				if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
					value, _ := strconv.Unquote(literal.Value)
					if strings.Contains(value, "https://api.vrchat.cloud") || strings.Contains(value, "wss://pipeline.vrchat.cloud") {
						t.Errorf("%s hardcodes a VRChat connection URL outside the SDK", path)
					}
				}
				call, ok := node.(*ast.CallExpr)
				if !ok || !server || filepath.Base(path) == "turnstile.go" {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selector.Sel.Name == "Do" {
					if receiver, ok := selector.X.(*ast.SelectorExpr); ok && receiver.Sel.Name == "Client" {
						t.Errorf("%s sends an HTTP request outside the SDK", path)
					}
				}
				if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "http" {
					switch selector.Sel.Name {
					case "NewRequest", "NewRequestWithContext", "Get", "Post", "PostForm":
						t.Errorf("%s constructs an HTTP request outside the SDK", path)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	err := filepath.WalkDir(filepath.Join("..", "..", "app"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || (!strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx")) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "https://api.vrchat.cloud") || strings.Contains(string(data), "wss://pipeline.vrchat.cloud") {
			t.Errorf("%s connects directly to VRChat from the frontend", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
