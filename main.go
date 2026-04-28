package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Hucaru/gonx"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// nxFile holds the parsed state of the currently loaded NX file.
type nxFile struct {
	path    string
	nodes   []gonx.Node
	strings []string
	bitmaps [][]byte
	audio   [][]byte
}

var (
	mu      sync.RWMutex
	current *nxFile
)

// nodeTypeName returns a human-readable type name.
func nodeTypeName(t uint16) string {
	switch t {
	case 0:
		return "none"
	case 1:
		return "int64"
	case 2:
		return "double"
	case 3:
		return "string"
	case 4:
		return "vector"
	case 5:
		return "bitmap"
	case 6:
		return "audio"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// nodeDataString formats the 8-byte data field of a node into a readable value.
func nodeDataString(n gonx.Node, strings []string) string {
	switch n.Type {
	case 0:
		return ""
	case 1:
		v := int64(binary.LittleEndian.Uint64(n.Data[:]))
		return fmt.Sprintf("%d", v)
	case 2:
		bits := binary.LittleEndian.Uint64(n.Data[:])
		return fmt.Sprintf("%g", math.Float64frombits(bits))
	case 3:
		id := binary.LittleEndian.Uint32(n.Data[:4])
		if int(id) < len(strings) {
			return fmt.Sprintf("%q", strings[id])
		}
		return fmt.Sprintf("string_id=%d", id)
	case 4:
		x := int32(binary.LittleEndian.Uint32(n.Data[:4]))
		y := int32(binary.LittleEndian.Uint32(n.Data[4:]))
		return fmt.Sprintf("(%d, %d)", x, y)
	case 5:
		id := binary.LittleEndian.Uint32(n.Data[:4])
		w := binary.LittleEndian.Uint16(n.Data[4:6])
		h := binary.LittleEndian.Uint16(n.Data[6:8])
		return fmt.Sprintf("bitmap_id=%d width=%d height=%d", id, w, h)
	case 6:
		id := binary.LittleEndian.Uint32(n.Data[:4])
		length := binary.LittleEndian.Uint32(n.Data[4:])
		return fmt.Sprintf("audio_id=%d length=%d", id, length)
	default:
		return fmt.Sprintf("%x", n.Data)
	}
}

// formatNode formats a single node into a single-line description.
func formatNode(n gonx.Node, strs []string, indent int) string {
	name := ""
	if int(n.NameID) < len(strs) {
		name = strs[n.NameID]
	}
	prefix := strings.Repeat("  ", indent)
	data := nodeDataString(n, strs)
	typeName := nodeTypeName(n.Type)
	if data != "" {
		return fmt.Sprintf("%s%s  [%s: %s]  children=%d", prefix, name, typeName, data, n.ChildCount)
	}
	return fmt.Sprintf("%s%s  [%s]  children=%d", prefix, name, typeName, n.ChildCount)
}

// printTree recursively builds a text tree up to maxDepth.
func printTree(n *gonx.Node, nodes []gonx.Node, strs []string, level, maxDepth int, sb *strings.Builder) {
	sb.WriteString(formatNode(*n, strs, level))
	sb.WriteByte('\n')
	if level >= maxDepth {
		return
	}
	for i := uint32(0); i < uint32(n.ChildCount); i++ {
		idx := n.ChildID + i
		if int(idx) >= len(nodes) {
			break
		}
		child := nodes[idx]
		printTree(&child, nodes, strs, level+1, maxDepth, sb)
	}
}

// walkSearch does a DFS and collects paths whose node name matches the pattern.
func walkSearch(n *gonx.Node, nodes []gonx.Node, strs []string, pattern, currentPath string, results *[]string, maxResults int) {
	if len(*results) >= maxResults {
		return
	}
	name := ""
	if int(n.NameID) < len(strs) {
		name = strs[n.NameID]
	}
	var nodePath string
	if currentPath == "" {
		nodePath = name
	} else {
		nodePath = currentPath + "/" + name
	}
	if strings.Contains(strings.ToLower(name), strings.ToLower(pattern)) {
		typeName := nodeTypeName(n.Type)
		data := nodeDataString(*n, strs)
		entry := fmt.Sprintf("%s  [%s]", nodePath, typeName)
		if data != "" {
			entry = fmt.Sprintf("%s  [%s: %s]", nodePath, typeName, data)
		}
		*results = append(*results, entry)
	}
	for i := uint32(0); i < uint32(n.ChildCount); i++ {
		idx := n.ChildID + i
		if int(idx) >= len(nodes) {
			break
		}
		child := nodes[idx]
		walkSearch(&child, nodes, strs, pattern, nodePath, results, maxResults)
	}
}

// ---- Tool handlers ----

func handleNxLoad(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filePath, err := req.RequireString("file")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	filePath = filepath.Clean(filePath)

	nodes, strs, bitmaps, audio, err := gonx.Parse(filePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to parse NX file: %v", err)), nil
	}

	mu.Lock()
	current = &nxFile{
		path:    filePath,
		nodes:   nodes,
		strings: strs,
		bitmaps: bitmaps,
		audio:   audio,
	}
	mu.Unlock()

	summary := fmt.Sprintf(
		"Loaded %s\n  nodes:   %d\n  strings: %d\n  bitmaps: %d\n  audio:   %d",
		filepath.Base(filePath),
		len(nodes),
		len(strs),
		len(bitmaps),
		len(audio),
	)
	return mcp.NewToolResultText(summary), nil
}

func handleNxListNode(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	mu.RLock()
	f := current
	mu.RUnlock()
	if f == nil {
		return mcp.NewToolResultError("no NX file loaded — call nx_load first"), nil
	}

	nodePath := req.GetString("path", "")
	depth := req.GetInt("depth", 1)
	if depth < 0 {
		depth = 0
	}
	if depth > 10 {
		depth = 10
	}

	var target *gonx.Node
	if nodePath == "" {
		root := f.nodes[0]
		target = &root
	} else {
		if !gonx.FindNode(nodePath, f.nodes, f.strings, func(n *gonx.Node) { target = n }) {
			return mcp.NewToolResultError(fmt.Sprintf("path not found: %s", nodePath)), nil
		}
	}

	var sb strings.Builder
	printTree(target, f.nodes, f.strings, 0, depth, &sb)
	return mcp.NewToolResultText(sb.String()), nil
}

func handleNxGetNode(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	mu.RLock()
	f := current
	mu.RUnlock()
	if f == nil {
		return mcp.NewToolResultError("no NX file loaded — call nx_load first"), nil
	}

	nodePath, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var target *gonx.Node
	if !gonx.FindNode(nodePath, f.nodes, f.strings, func(n *gonx.Node) { target = n }) {
		return mcp.NewToolResultError(fmt.Sprintf("path not found: %s", nodePath)), nil
	}

	name := ""
	if int(target.NameID) < len(f.strings) {
		name = f.strings[target.NameID]
	}
	typeName := nodeTypeName(target.Type)
	data := nodeDataString(*target, f.strings)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("name:     %s\n", name))
	sb.WriteString(fmt.Sprintf("type:     %s\n", typeName))
	if data != "" {
		sb.WriteString(fmt.Sprintf("data:     %s\n", data))
	}
	sb.WriteString(fmt.Sprintf("children: %d\n", target.ChildCount))
	if target.ChildCount > 0 {
		sb.WriteString("child names:\n")
		for i := uint32(0); i < uint32(target.ChildCount); i++ {
			idx := target.ChildID + i
			if int(idx) >= len(f.nodes) {
				break
			}
			child := f.nodes[idx]
			childName := ""
			if int(child.NameID) < len(f.strings) {
				childName = f.strings[child.NameID]
			}
			sb.WriteString(fmt.Sprintf("  - %s  [%s]\n", childName, nodeTypeName(child.Type)))
		}
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func handleNxSearch(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	mu.RLock()
	f := current
	mu.RUnlock()
	if f == nil {
		return mcp.NewToolResultError("no NX file loaded — call nx_load first"), nil
	}

	pattern, err := req.RequireString("pattern")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	maxResults := req.GetInt("max_results", 50)
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 500 {
		maxResults = 500
	}

	root := f.nodes[0]
	var results []string
	walkSearch(&root, f.nodes, f.strings, pattern, "", &results, maxResults)

	if len(results) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("no nodes found matching %q", pattern)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("found %d result(s) for %q", len(results), pattern))
	if len(results) == maxResults {
		sb.WriteString(fmt.Sprintf(" (limit %d reached)", maxResults))
	}
	sb.WriteByte('\n')
	for _, r := range results {
		sb.WriteString(r)
		sb.WriteByte('\n')
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func main() {
	s := server.NewMCPServer(
		"nx-mcp",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	// nx_load — load an NX file into memory
	s.AddTool(
		mcp.NewTool("nx_load",
			mcp.WithDescription("Load an NX (PKG4) file from disk into memory. Must be called before any other nx_* tool."),
			mcp.WithString("file",
				mcp.Required(),
				mcp.Description("Absolute or relative path to the .nx file to load."),
			),
		),
		handleNxLoad,
	)

	// nx_list_node — print a subtree starting at a path
	s.AddTool(
		mcp.NewTool("nx_list_node",
			mcp.WithDescription("List the children of a node in the loaded NX file. Omit 'path' to start from the root node."),
			mcp.WithString("path",
				mcp.Description("Slash-separated node path, e.g. \"Character/00002000.img\". Leave empty for root."),
			),
			mcp.WithNumber("depth",
				mcp.Description("How many levels of children to expand (1–10, default 1)."),
			),
		),
		handleNxListNode,
	)

	// nx_get_node — inspect a single node in detail
	s.AddTool(
		mcp.NewTool("nx_get_node",
			mcp.WithDescription("Get detailed information about a specific node: its type, data value, and immediate children."),
			mcp.WithString("path",
				mcp.Required(),
				mcp.Description("Slash-separated node path, e.g. \"Character/00002000.img/stand1/0\"."),
			),
		),
		handleNxGetNode,
	)

	// nx_search — search for nodes by name
	s.AddTool(
		mcp.NewTool("nx_search",
			mcp.WithDescription("Search all nodes in the loaded NX file for names containing a pattern (case-insensitive substring match). Returns matching full paths."),
			mcp.WithString("pattern",
				mcp.Required(),
				mcp.Description("Substring to search for in node names."),
			),
			mcp.WithNumber("max_results",
				mcp.Description("Maximum number of results to return (1–500, default 50)."),
			),
		),
		handleNxSearch,
	)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
