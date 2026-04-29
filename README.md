# nx-mcp

An [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) server for interacting with **NX (PKG4)** data files — the binary node-tree format used by MapleStory clients.

## Tools

| Tool | Description |
|------|-------------|
| `nx_load` | Load an `.nx` file from disk into memory. Must be called before any other tool. |
| `nx_list_node` | List the children of a node at a given path. Omit `path` to start at the root. Supports a `depth` parameter (1–10). |
| `nx_get_node` | Get the type, data value, and immediate children of a specific node. |
| `nx_search` | Search all node names for a case-insensitive substring. Returns matching full paths. |

### Node types

| Type ID | Name | Data |
|---------|------|------|
| 0 | `none` | — |
| 1 | `int64` | 64-bit signed integer |
| 2 | `double` | 64-bit IEEE double |
| 3 | `string` | String value (resolved from string table) |
| 4 | `vector` | (x, y) pair of 32-bit signed integers |
| 5 | `bitmap` | Bitmap ID, width, height |
| 6 | `audio` | Audio ID, data length |

## Usage

### Building from source

```bash
git clone https://github.com/ErwinsExpertise/nx-mcp
cd nx-mcp
go build -o nx-mcp .
```

### Running directly

```bash
./nx-mcp
```

The server communicates over **stdio** using the MCP JSON-RPC protocol and can be connected to any MCP-compatible client.

### Example session

```
nx_load   file="/path/to/Data.nx"
  → Loaded Data.nx
      nodes:   4439446
      strings: 25921
      bitmaps: 0
      audio:   0

nx_list_node   path=""   depth=1
  →   [none]  children=14
        Character  [none]  children=25
        Effect     [none]  children=10
        ...

nx_get_node   path="Character/00002000.img"
  → name:     00002000.img
    type:     none
    children: 4
    child names:
      - stand1  [none]
      ...

nx_search   pattern="stand"   max_results=20
  → found 3 result(s) for "stand"
    Character/00002000.img/stand1  [none]
    ...
```

## OpenCode configuration

Add the server to your `opencode.json` (place it in your project directory or `~/.config/opencode/`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "nx-mcp": {
      "type": "local",
      "command": ["go", "run", "github.com/ErwinsExpertise/nx-mcp@latest"]
    }
  }
}
```

If you have already built the binary:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "nx-mcp": {
      "type": "local",
      "command": ["/path/to/nx-mcp"]
    }
  }
}
```

The included [`opencode.json`](./opencode.json) in the repository root uses `go run` and can be used directly when developing inside this repository.

## NX format

The server reads **PKG4** (NX format v1) files. The format specification is at <https://nxformat.github.io/>.

Key facts:
- Little-endian, with LZ4-compressed bitmap data
- 52-byte header, 20-byte nodes
- Separate offset tables for strings, bitmaps, and audio

## References

- [NX format specification](https://nxformat.github.io/)
- [gonx](https://github.com/Hucaru/gonx) — Go NX parser library used by this server
- [nxpeek.go](https://github.com/ErwinsExpertise/ValhallaV48/blob/main/scripts/nxpeek.go) — CLI NX inspector (inspiration)
- [WzImg-MCP-Server](https://github.com/lastbattle/WzImg-MCP-Server) — similar MCP server for WZ/IMG files