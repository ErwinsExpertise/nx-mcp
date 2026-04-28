package main

import (
"bytes"
"context"
"encoding/binary"
"os"
"strings"
"testing"

"github.com/mark3labs/mcp-go/mcp"
)

// buildMinimalNX constructs a minimal valid PKG4 NX file and writes it to a temp file.
// The tree has the following nodes (id: name, type):
//
//0: ""      none  (root, 3 children starting at id 1)
//1: "Character"  none  (1 child at id 4)
//2: "testInt"    int64  data=42
//3: "testString" string data=string_id 4
//4: "00002000.img" none  (child of Character)
//
// Strings: 0="", 1="Character", 2="testInt", 3="testString", 4="hello", 5="00002000.img"
func buildMinimalNX(t *testing.T) string {
t.Helper()

const (
nodeCount    = 5
stringCount  = 6
headerSize   = 52
nodeSize     = 20
nodesEnd     = headerSize + nodeCount*nodeSize // 152
strTableSize = stringCount * 8                // 48
strDataStart = nodesEnd + strTableSize        // 200
)

// Build string data to know exact offsets.
type strEntry struct {
offset uint64
data   []byte
}
rawStrings := []string{"", "Character", "testInt", "testString", "hello", "00002000.img"}
var strEntries []strEntry
offset := uint64(strDataStart)
for _, s := range rawStrings {
strEntries = append(strEntries, strEntry{offset: offset, data: []byte(s)})
offset += 2 + uint64(len(s))
}
fileSize := int(offset)

buf := make([]byte, fileSize)
w := func(off int, v interface{}) {
var tmp bytes.Buffer
_ = binary.Write(&tmp, binary.LittleEndian, v)
copy(buf[off:], tmp.Bytes())
}

// Header
copy(buf[0:4], []byte{0x50, 0x4B, 0x47, 0x34}) // magic "PKG4"
w(4, uint32(nodeCount))
w(8, int64(headerSize))          // NodeBlockOffset
w(16, uint32(stringCount))
w(24, int64(nodesEnd))           // StringOffsetTableOffset
w(32, uint32(0))                 // BitmapCount = 0
w(40, int64(0))                  // BitmapOffsetTableOffset (ignored)
w(48, uint32(0))                 // AudioCount = 0
// AudioOffsetTableOffset at offset 56 but header struct only goes to 52.
// Actually the header is exactly 52 bytes; gonx reads AudioOffsetTableOffset as the 9th field.
// Let us recalculate using gonx's header struct field order:
// [4]byte + uint32 + int64 + uint32 + int64 + uint32 + int64 + uint32 + int64
// offsets:   0        4       8       16      24       32      40       48      56 ... but that's 64 bytes?
// No: 4+4+8+4+8+4+8+4+8 = 52. Let's recount:
// 0: [4]byte  -> 4 bytes  -> end=4
// 4: uint32   -> 4 bytes  -> end=8
// 8: int64    -> 8 bytes  -> end=16
// 16: uint32  -> 4 bytes  -> end=20
// 20: int64   -> 8 bytes  -> end=28
// 28: uint32  -> 4 bytes  -> end=32
// 32: int64   -> 8 bytes  -> end=40
// 40: uint32  -> 4 bytes  -> end=44
// 44: int64   -> 8 bytes  -> end=52
// => AudioOffsetTableOffset is at byte offset 44

// Re-write with correct offsets
buf = make([]byte, fileSize)
copy(buf[0:4], []byte{0x50, 0x4B, 0x47, 0x34})
binary.LittleEndian.PutUint32(buf[4:], uint32(nodeCount))
binary.LittleEndian.PutUint64(buf[8:], uint64(headerSize))
binary.LittleEndian.PutUint32(buf[16:], uint32(stringCount))
binary.LittleEndian.PutUint64(buf[20:], uint64(nodesEnd))
binary.LittleEndian.PutUint32(buf[28:], 0) // BitmapCount
binary.LittleEndian.PutUint64(buf[32:], 0) // BitmapOffsetTableOffset
binary.LittleEndian.PutUint32(buf[40:], 0) // AudioCount
binary.LittleEndian.PutUint64(buf[44:], 0) // AudioOffsetTableOffset

// Nodes (20 bytes each, starting at offset headerSize=52)
writeNode := func(idx int, nameID, childID uint32, childCount, typ uint16, data [8]byte) {
base := headerSize + idx*nodeSize
binary.LittleEndian.PutUint32(buf[base:], nameID)
binary.LittleEndian.PutUint32(buf[base+4:], childID)
binary.LittleEndian.PutUint16(buf[base+8:], childCount)
binary.LittleEndian.PutUint16(buf[base+10:], typ)
copy(buf[base+12:], data[:])
}

var zeroData [8]byte
writeNode(0, 0, 1, 3, 0, zeroData) // root: 3 children starting at id 1
writeNode(1, 1, 4, 1, 0, zeroData) // Character: 1 child at id 4
var int64Data [8]byte
binary.LittleEndian.PutUint64(int64Data[:], uint64(42))
writeNode(2, 2, 0, 0, 1, int64Data) // testInt: int64=42
var strData [8]byte
binary.LittleEndian.PutUint32(strData[:], 4) // string_id=4 ("hello")
writeNode(3, 3, 0, 0, 3, strData) // testString: string="hello"
writeNode(4, 5, 0, 0, 0, zeroData) // 00002000.img: no children

// String offset table (at offset nodesEnd=152, 6 * 8 = 48 bytes)
for i, e := range strEntries {
binary.LittleEndian.PutUint64(buf[nodesEnd+i*8:], e.offset)
}

// String data
for _, e := range strEntries {
off := int(e.offset)
binary.LittleEndian.PutUint16(buf[off:], uint16(len(e.data)))
copy(buf[off+2:], e.data)
}

f, err := os.CreateTemp(t.TempDir(), "test_*.nx")
if err != nil {
t.Fatal(err)
}
if _, err := f.Write(buf); err != nil {
t.Fatal(err)
}
name := f.Name()
f.Close()
return name
}

func resetState() {
mu.Lock()
current = nil
mu.Unlock()
}

func TestNodeTypeName(t *testing.T) {
cases := []struct {
typ  uint16
want string
}{
{0, "none"},
{1, "int64"},
{2, "double"},
{3, "string"},
{4, "vector"},
{5, "bitmap"},
{6, "audio"},
{99, "unknown(99)"},
}
for _, c := range cases {
if got := nodeTypeName(c.typ); got != c.want {
t.Errorf("nodeTypeName(%d) = %q, want %q", c.typ, got, c.want)
}
}
}

func TestHandleNxLoadBadFile(t *testing.T) {
resetState()
req := mcp.CallToolRequest{}
req.Params.Arguments = map[string]any{"file": "/tmp/does_not_exist_nx_test.nx"}
result, err := handleNxLoad(context.Background(), req)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if !result.IsError {
t.Fatal("expected error result for missing file")
}
}

func TestHandleNxLoad(t *testing.T) {
resetState()
path := buildMinimalNX(t)

req := mcp.CallToolRequest{}
req.Params.Arguments = map[string]any{"file": path}
result, err := handleNxLoad(context.Background(), req)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if result.IsError {
t.Fatalf("load failed: %v", result.Content)
}
text := result.Content[0].(mcp.TextContent).Text
if !strings.Contains(text, "nodes:   5") {
t.Errorf("expected 5 nodes in output, got: %s", text)
}
if !strings.Contains(text, "strings: 6") {
t.Errorf("expected 6 strings in output, got: %s", text)
}
}

func TestHandleNxListNodeNoFile(t *testing.T) {
resetState()
req := mcp.CallToolRequest{}
req.Params.Arguments = map[string]any{}
result, err := handleNxListNode(context.Background(), req)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if !result.IsError {
t.Fatal("expected error when no file loaded")
}
}

func TestHandleNxListNodeRoot(t *testing.T) {
resetState()
path := buildMinimalNX(t)

loadReq := mcp.CallToolRequest{}
loadReq.Params.Arguments = map[string]any{"file": path}
if _, err := handleNxLoad(context.Background(), loadReq); err != nil {
t.Fatal(err)
}

req := mcp.CallToolRequest{}
req.Params.Arguments = map[string]any{"depth": float64(1)}
result, err := handleNxListNode(context.Background(), req)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if result.IsError {
t.Fatalf("list node failed: %v", result.Content)
}
text := result.Content[0].(mcp.TextContent).Text
if !strings.Contains(text, "Character") {
t.Errorf("expected 'Character' in root listing, got: %s", text)
}
}

func TestHandleNxListNodePath(t *testing.T) {
resetState()
path := buildMinimalNX(t)

loadReq := mcp.CallToolRequest{}
loadReq.Params.Arguments = map[string]any{"file": path}
if _, err := handleNxLoad(context.Background(), loadReq); err != nil {
t.Fatal(err)
}

req := mcp.CallToolRequest{}
req.Params.Arguments = map[string]any{"path": "Character", "depth": float64(1)}
result, err := handleNxListNode(context.Background(), req)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if result.IsError {
t.Fatalf("list node failed: %v", result.Content)
}
text := result.Content[0].(mcp.TextContent).Text
if !strings.Contains(text, "00002000.img") {
t.Errorf("expected '00002000.img' under Character, got: %s", text)
}
}

func TestHandleNxListNodeInvalidPath(t *testing.T) {
resetState()
path := buildMinimalNX(t)

loadReq := mcp.CallToolRequest{}
loadReq.Params.Arguments = map[string]any{"file": path}
if _, err := handleNxLoad(context.Background(), loadReq); err != nil {
t.Fatal(err)
}

req := mcp.CallToolRequest{}
req.Params.Arguments = map[string]any{"path": "NoSuchNode"}
result, err := handleNxListNode(context.Background(), req)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if !result.IsError {
t.Fatal("expected error for invalid path")
}
}

func TestHandleNxGetNode(t *testing.T) {
resetState()
path := buildMinimalNX(t)

loadReq := mcp.CallToolRequest{}
loadReq.Params.Arguments = map[string]any{"file": path}
if _, err := handleNxLoad(context.Background(), loadReq); err != nil {
t.Fatal(err)
}

req := mcp.CallToolRequest{}
req.Params.Arguments = map[string]any{"path": "testInt"}
result, err := handleNxGetNode(context.Background(), req)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if result.IsError {
t.Fatalf("get node failed: %v", result.Content)
}
text := result.Content[0].(mcp.TextContent).Text
if !strings.Contains(text, "int64") {
t.Errorf("expected type 'int64', got: %s", text)
}
if !strings.Contains(text, "42") {
t.Errorf("expected value 42, got: %s", text)
}
}

func TestHandleNxGetNodeString(t *testing.T) {
resetState()
path := buildMinimalNX(t)

loadReq := mcp.CallToolRequest{}
loadReq.Params.Arguments = map[string]any{"file": path}
if _, err := handleNxLoad(context.Background(), loadReq); err != nil {
t.Fatal(err)
}

req := mcp.CallToolRequest{}
req.Params.Arguments = map[string]any{"path": "testString"}
result, err := handleNxGetNode(context.Background(), req)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if result.IsError {
t.Fatalf("get node failed: %v", result.Content)
}
text := result.Content[0].(mcp.TextContent).Text
if !strings.Contains(text, "hello") {
t.Errorf("expected string value 'hello', got: %s", text)
}
}

func TestHandleNxSearch(t *testing.T) {
resetState()
path := buildMinimalNX(t)

loadReq := mcp.CallToolRequest{}
loadReq.Params.Arguments = map[string]any{"file": path}
if _, err := handleNxLoad(context.Background(), loadReq); err != nil {
t.Fatal(err)
}

req := mcp.CallToolRequest{}
req.Params.Arguments = map[string]any{"pattern": "test", "max_results": float64(10)}
result, err := handleNxSearch(context.Background(), req)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if result.IsError {
t.Fatalf("search failed: %v", result.Content)
}
text := result.Content[0].(mcp.TextContent).Text
if !strings.Contains(text, "testInt") {
t.Errorf("expected 'testInt' in search results, got: %s", text)
}
if !strings.Contains(text, "testString") {
t.Errorf("expected 'testString' in search results, got: %s", text)
}
}

func TestHandleNxSearchNoMatch(t *testing.T) {
resetState()
path := buildMinimalNX(t)

loadReq := mcp.CallToolRequest{}
loadReq.Params.Arguments = map[string]any{"file": path}
if _, err := handleNxLoad(context.Background(), loadReq); err != nil {
t.Fatal(err)
}

req := mcp.CallToolRequest{}
req.Params.Arguments = map[string]any{"pattern": "zzz_no_match"}
result, err := handleNxSearch(context.Background(), req)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if result.IsError {
t.Fatalf("unexpected tool error: %v", result.Content)
}
text := result.Content[0].(mcp.TextContent).Text
if !strings.Contains(text, "no nodes found") {
t.Errorf("expected 'no nodes found' message, got: %s", text)
}
}
