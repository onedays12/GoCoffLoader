# GoCoffLoader

English | [中文](README.zh-CN.md)

# Introduction

As more C2 implants move core features into BOFs, a reliable BOF loader becomes increasingly important. This project is an experimental Go BOF/COFF loader created for learning, testing, and improving BOF execution from Go.

The project is still immature. Issues, reviews, and pull requests are welcome.

# Install

```powershell
go get github.com/onedays12/GoCoffLoader@v0.1.0
```

# Usage

```go
import "github.com/onedays12/GoCoffLoader/pkg/coff"

argBytes := coff.PackArgs([]interface{}{
    "hello",
    uint32(123),
    []byte{0x41, 0x42, 0x43},
})

output, err := coff.LoadWithMethod(data, argBytes, "go")
if err != nil {
    // handle error
}
fmt.Println(output)
```

You can also use the default `go` entry point:

```go
output, err := coff.Load(data, argBytes)
```

# Args

The BOF entry point follows the common Beacon BOF style:

```c
void go(char *args, int len);
```

The Go side passes a packed `[]byte`. Use `coff.PackArgs`:

```go
argBytes := coff.PackArgs([]interface{}{
    "user",
    uint32(100),
    int(200),
    []byte("raw-data"),
})

output, err := coff.LoadWithMethod(coffBytes, argBytes, "go")
```

Supported `PackArgs` types:

- `string`: writes `uint32(length)` + string bytes + `\x00`
- `[]byte`: writes `uint32(length)` + raw bytes
- `uint32`: writes `uint32(4)` + 4-byte little-endian integer
- `int`: converts to `uint32`, then writes `uint32(4)` + 4-byte little-endian integer

The full argument buffer layout:

```text
uint32 total_size
uint32 arg1_size
byte   arg1_data[arg1_size]
uint32 arg2_size
byte   arg2_data[arg2_size]
...
```

`total_size` is the full buffer length, including the first 4 bytes. After the BOF calls `BeaconDataParse(&parser, args, len)`, the parser skips the first 4 bytes and starts reading from the first `arg_size`.

Mapping to Beacon APIs:

- `BeaconDataExtract`: reads `uint32 size`, then returns a pointer to the next `size` bytes.
- `BeaconDataInt`: skips the current `uint32 size`, then reads the next 4 bytes as an integer.
- `BeaconDataShort`: skips the current `uint32 size`, then reads the next 2 bytes as a short integer.

BOF example:

```c
void go(char *args, int len) {
    datap parser;
    BeaconDataParse(&parser, args, len);

    int userLen = 0;
    char *user = BeaconDataExtract(&parser, &userLen);
    int count = BeaconDataInt(&parser);

    BeaconPrintf(CALLBACK_OUTPUT, "user=%s count=%d", user, count);
}
```

# Notes

- Windows amd64 only
- Executes BOF/COFF in-process
- Native crashes may affect the host process depending on execution mode

# Reference

- https://github.com/RIscRIpt/pecoff
- https://github.com/praetorian-inc/goffloader
- https://github.com/Cen4enCen/CenCoffLdr/stargazers
